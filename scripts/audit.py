#!/usr/bin/env python3
"""Внешние приёмочные и нагрузочные тесты. Python нужен для проверок, а не для работы приложения."""
import argparse
import contextlib
import fcntl
import os
import pty
import select
import socket
import struct
import subprocess
import tempfile
import termios
import threading
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / "TCPChat"
CHECKS = 0
os.environ.setdefault("GORACE", "halt_on_error=1")


def check(ok, label):
    global CHECKS
    if not ok:
        raise AssertionError(label)
    CHECKS += 1
    print("PASS", label, flush=True)


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class Peer:
    def __init__(self, port, name=None):
        self.sock = socket.create_connection(("127.0.0.1", port), 5)
        self.buffer = b""
        self.hello = self.until("[ENTER YOUR NAME]:")
        if name is not None:
            self.send(name)
            self.history = self.until("[System]: Welcome " + name + " |")

    def send(self, message):
        self.sock.sendall((message + "\n").encode())

    def line(self, timeout=10):
        deadline = time.monotonic() + timeout
        while b"\n" not in self.buffer:
            self.sock.settimeout(max(0.01, deadline - time.monotonic()))
            data = self.sock.recv(65536)
            if not data:
                raise EOFError("peer closed")
            self.buffer += data
        line, self.buffer = self.buffer.split(b"\n", 1)
        return line.decode()

    def until(self, marker):
        lines = []
        for _ in range(100000):
            line = self.line()
            lines.append(line)
            if marker in line:
                return lines
        raise AssertionError("missing marker: " + marker)

    def profile(self):
        self.send("/profile")
        return self.until("Session profile |")

    def close(self):
        self.sock.close()


@contextlib.contextmanager
def server(port=None, default=False):
    if port is None:
        port = free_port()
    with tempfile.TemporaryDirectory(prefix="net-link-audit-") as directory:
        args = [] if default else [str(port)]
        proc = subprocess.Popen([str(BINARY)] + args, cwd=directory,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            if not select.select([proc.stdout], [], [], 8)[0]:
                raise AssertionError("server startup timeout")
            line = proc.stdout.readline().decode().strip()
            if line != "Listening on the port :" + str(port):
                raise AssertionError((line, proc.poll(), proc.stderr.read().decode() if proc.poll() is not None else ""))
            yield port, Path(directory), proc
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait()
            diagnostic = proc.stderr.read()
            proc.stdout.close()
            proc.stderr.close()
            if b"WARNING: DATA RACE" in diagnostic:
                raise AssertionError(diagnostic.decode(errors="replace"))


def basic():
    invalid = subprocess.run([str(BINARY), "2525", "localhost"], capture_output=True)
    check(invalid.returncode == 2 and invalid.stdout == b"[USAGE]: ./TCPChat $port\n" and not invalid.stderr, "exact invalid-argument usage")
    for port in (8989, 2525):
        with server(port, default=port == 8989) as (port, _, _):
            p = Peer(port)
            check(p.hello[0] == "Welcome to TCP-Chat!" and "        dGGGGMMb" in p.hello, "port %d, Linux logo, name prompt" % port)
            p.close()
    with server() as (port, directory, proc):
        a, b = Peer(port, "GhostOfAstana"), Peer(port, "Tomorrow")
        try:
            check(a.history[0] == "[System]: Session profile | pace: quiet | no messages yet", "exact empty-session profile")
            check("Tomorrow has joined our chat." in a.line(), "join notification")
            a.send("Сәлем, Tomorrow School!")
            check("][GhostOfAstana]:Сәлем, Tomorrow School!" in b.line(), "UTF-8 timestamped broadcast")
            barrier = a.profile()
            check(len(barrier) == 1 and "total: 1 msgs" in barrier[0], "no echo to sender")
            a.send(""); a.send("   ")
            check("total: 1 msgs" in a.profile()[-1], "empty lines ignored")
            c = Peer(port, "Audit")
            check(len(c.history) == 3 and "][GhostOfAstana]:" in c.history[0] and c.history[1].startswith("[System]: Session profile | pace: lively |"), "complete history immediately followed by live profile")
            a.until("Audit has joined"); b.until("Audit has joined")
            b.send("three-client-test")
            check("[Tomorrow]:three-client-test" in a.line() and "[Tomorrow]:three-client-test" in c.line(), "both other clients receive identical chat message")
            b.close(); a.until("Tomorrow has left"); c.until("Tomorrow has left")
            a.send("after-disconnect")
            check("[GhostOfAstana]:after-disconnect" in c.line(), "disconnect leaves other clients working")
            a.send("/nick Veteran"); a.until("is now known as Veteran"); c.until("is now known as Veteran")
            check("top talker: Veteran (2 msgs)" in a.profile()[-1], "rename notifies room and preserves counters")
            a.send("/join dojo"); a.until("room: dojo"); c.until("Veteran has left")
            a.send("practice-since-2008")
            check("total: 1 msgs" in a.profile()[-1] and "total: 3 msgs" in c.profile()[-1], "rooms isolate messages, history and counters")
            c.send("/join dojo"); h = c.until("room: dojo")
            check(len(h) == 3 and "practice-since-2008" in h[0], "new room replays its own history")
            a.until("Audit has joined")
            for i in range(6):
                c.send("flood-%d" % i); a.until("[Audit]:flood-")
            check(c.line() == "[System]: You are sending messages too fast. Please slow down.", "private sixth-message warning")
            check(len(a.profile()) == 1, "flood warning not broadcast")
            c.close(); a.until("Audit has left")
            a.send("/join lobby"); a.until("room: lobby")
            a.send("bad\x1b[2J"); a.until("Message rejected")
            check("total: 3 msgs" in a.profile()[-1], "terminal controls rejected without counting")
            log = (directory / "logs/net-link.log").read_text()
            check("practice-since-2008" in log and "is now known as Veteran" in log and "has left our chat" in log, "persistent activity and message logs")
            check(proc.poll() is None, "server alive after malformed inputs and room changes")
        finally:
            a.close(); b.close()


def capacity():
    with server() as (port, _, _):
        peers = [Peer(port) for _ in range(10)]
        try:
            with socket.create_connection(("127.0.0.1", port), 5) as rejected:
                check(b"Server is full (10 connections)" in rejected.recv(4096), "eleventh connection rejected, including unnamed clients")
            peers.pop().close()
            time.sleep(0.05)
            p = Peer(port, "Replacement"); p.close()
            check(True, "released connection slot reused")
        finally:
            for p in peers: p.close()


def load(messages):
    with server() as (port, _, _):
        peers = [Peer(port, "P%d" % i) for i in range(8)]
        observer = Peer(port, "Observer")
        received = [[] for _ in range(9)]
        failures = []
        expected = messages * 8
        def read(index, p, target):
            try:
                while len(received[index]) < target:
                    line = p.line(20)
                    if "]:load-" in line:
                        received[index].append(line)
            except Exception as exc:
                failures.append(repr(exc))
        readers = [threading.Thread(target=read, args=(i,p,messages*7)) for i,p in enumerate(peers)]
        readers.append(threading.Thread(target=read, args=(8,observer,expected)))
        for thread in readers: thread.start()
        def send(i, p):
            try:
                for j in range(messages): p.send("load-%d-%d-" % (i,j) + "x"*240)
            except Exception as exc:
                failures.append(repr(exc))
        writers = [threading.Thread(target=send, args=(i,p)) for i,p in enumerate(peers)]
        for thread in writers: thread.start()
        for thread in writers + readers: thread.join(30)
        try:
            check(not failures and not any(t.is_alive() for t in writers+readers), "concurrent load finishes without socket failure")
            check(len(set(received[8])) == expected, "%d messages delivered once to observer" % expected)
            for i in range(8):
                check(len(set(received[i])) == messages*7 and all("[P%d]:" % i not in line for line in received[i]), "sender %d receives every other message, no self echo" % i)
            ordered = received[8]
            for i in range(8):
                check(received[i] == [line for line in ordered if "[P%d]:" % i not in line], "consistent global order for client %d" % i)
            newcomer = Peer(port, "Historian")
            check(newcomer.history[:-2] == ordered and "total: %d msgs" % expected in newcomer.history[-2], "tenth client replays entire load history in order")
            newcomer.close()
        finally:
            for p in peers+[observer]: p.close()
            for thread in writers+readers: thread.join(2)


def replay_during_live():
    with server() as (port, _, _):
        a = Peer(port, "Author")
        # Читаем личные предупреждения, пока создаём историю размером больше буферов TCP.
        stop = threading.Event()
        def drain():
            try:
                while not stop.is_set(): a.line(20)
            except (OSError, EOFError): pass
        thread = threading.Thread(target=drain, daemon=True); thread.start()
        count = 2000
        for i in range(count): a.send("archive-%04d-" % i + "x"*3800)
        # Отдельный читатель подключается, пока сервер ещё может принимать сообщения.
        # За сохранённой частью истории идёт профиль, затем — новые сообщения в правильном порядке.
        b = Peer(port, "Replay")
        lines = [line for line in b.history if "][Author]:archive-" in line]
        while len(lines) < count:
            line = b.line(20)
            if "][Author]:archive-" in line: lines.append(line)
        check(len(lines) == count and all("archive-%04d-" % i in line for i,line in enumerate(lines)), "7.6 MB replay concurrent with live traffic: no gap or duplicate")
        check("total: 2000 msgs" in b.profile()[-1], "large-history counters remain exact")
        b.close(); stop.set(); a.close(); thread.join(2)


def client_flags():
    with server() as (port, _, _):
        result = subprocess.run([str(BINARY), "-c", "-z", "-v", "-w", "2", "127.0.0.1", str(port)], capture_output=True, timeout=5)
        check(result.returncode == 0 and b"Connected to" in result.stderr and not result.stdout, "-c -z -v -w probe flags")
    port = free_port()
    result = subprocess.run([str(BINARY), "-c", "127.0.0.1", str(port)], input=b"", capture_output=True, timeout=5)
    check(result.returncode == 1 and b"net-link:" in result.stderr, "client reports connection refusal")
    with socket.socket() as listener:
        listener.bind(("127.0.0.1",0)); listener.listen()
        def echo_after_eof():
            conn,_ = listener.accept()
            with conn:
                data = b""
                while True:
                    block = conn.recv(4096)
                    if not block: break
                    data += block
                conn.sendall(b"reply:"+data)
        worker=threading.Thread(target=echo_after_eof);worker.start()
        result = subprocess.run([str(BINARY), "-c", "-N", "127.0.0.1", str(listener.getsockname()[1])], input=b"half-close", capture_output=True, timeout=5)
        worker.join(2)
        check(result.returncode == 0 and result.stdout == b"reply:half-close", "-N half-close drains reply after input EOF")


def tui():
    with server() as (port, _, _):
        peer = Peer(port, "Witness")
        master, slave = pty.openpty()
        def size(w,h): fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", h,w,0,0))
        size(100,30)
        def session():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)
        proc = subprocess.Popen([str(BINARY), "--tui", "127.0.0.1", str(port)], stdin=slave, stdout=slave, stderr=slave, preexec_fn=session, env=dict(os.environ,TERM="xterm-256color"))
        os.close(slave)
        transcript = bytearray()
        stopped=threading.Event()
        def capture():
            try:
                while not stopped.is_set():
                    if select.select([master],[],[],0.2)[0]: transcript.extend(os.read(master,65536))
            except OSError: pass
        reader=threading.Thread(target=capture);reader.start()
        try:
            time.sleep(0.5)
            os.write(master,b"TerminalUser\r")
            peer.until("TerminalUser has joined")
            os.write(master,b"from-the-tui\r")
            check("[TerminalUser]:from-the-tui" in peer.line(), "real gocui PTY: name and message submission")
            os.write(master,b"/nick Operator\r")
            peer.until("TerminalUser is now known as Operator")
            os.write(master,b"after-");time.sleep(0.1)
            size(25,8);time.sleep(0.15);size(100,30);time.sleep(0.15)
            os.write(master,b"resize\r")
            check("[Operator]:after-resize" in peer.line(), "TUI preserves draft through small-terminal resize and nickname change")
            os.write(master,b"\x1b[5~\x1b[6~") # Клавиши прокрутки страницы вверх и вниз.
            time.sleep(0.1)
            os.write(master,b"\x03")
            proc.wait(timeout=5)
            check(proc.returncode == 0 and b"NET-LINK" in transcript, "TUI renders identity header and exits cleanly on Ctrl-C")
        finally:
            if proc.poll() is None: proc.kill();proc.wait()
            stopped.set();reader.join(2);os.close(master);peer.close()


if __name__ == "__main__":
    parser=argparse.ArgumentParser()
    parser.add_argument("--stress", action="store_true", help="include 800-message concurrent load and 7.6 MB history")
    parser.add_argument("--tui", action="store_true", help="exercise actual gocui through a pseudo-terminal")
    args=parser.parse_args()
    basic();capacity();client_flags()
    if args.stress: load(100);replay_during_live()
    if args.tui: tui()
    print("PASS: %d black-box checks" % CHECKS)
