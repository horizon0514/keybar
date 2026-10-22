package approval

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// The app listens on the loopback interface, on a port it chooses, and
// writes the port and a random token into ipc.json of the data folder,
// readable only by the user. A client sends one line, its token and its
// request, and reads one line, the decision.

const ipcFile = "ipc.json"

// Timeout is how long a request waits for the user.
const Timeout = 2 * time.Minute

type endpoint struct {
	Addr  string `json:"addr"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

type wireRequest struct {
	Token   string  `json:"token"`
	Request Request `json:"request"`
}

// Server takes requests for the app, which Handle decides: it may block
// until the user answers.
type Server struct {
	ln     net.Listener
	dir    string
	token  string
	Handle func(Request) Decision
}

// Listen starts the server, and tells clients of dir where it is.
func Listen(dir string, handle func(Request) Decision) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	s := &Server{ln: ln, dir: dir, token: hex.EncodeToString(b), Handle: handle}
	ep, _ := json.Marshal(endpoint{Addr: ln.Addr().String(), Token: s.token, PID: os.Getpid()})
	if err := os.MkdirAll(dir, 0o700); err != nil {
		ln.Close()
		return nil, err
	}
	tmp := filepath.Join(dir, ipcFile+".tmp")
	if err := os.WriteFile(tmp, ep, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	if err := os.Rename(tmp, filepath.Join(dir, ipcFile)); err != nil {
		ln.Close()
		return nil, err
	}
	go s.serve()
	return s, nil
}

func (s *Server) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return
	}
	var w wireRequest
	if json.Unmarshal(line, &w) != nil || subtle.ConstantTimeCompare([]byte(w.Token), []byte(s.token)) != 1 {
		_ = json.NewEncoder(conn).Encode(Decision{Why: "bad-token"})
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	d := s.Handle(w.Request)
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_ = json.NewEncoder(conn).Encode(d)
}

// Close stops the server, and forgets where it was.
func (s *Server) Close() error {
	_ = os.Remove(filepath.Join(s.dir, ipcFile))
	return s.ln.Close()
}

// ErrNoApp is the app not running.
var ErrNoApp = errors.New("KeyBar 没有在运行")

// Client asks the app of dir.
type Client struct {
	Dir     string
	Timeout time.Duration // Timeout by default
}

// Approve asks the app; ErrNoApp when it does not run.
func (c Client) Approve(r Request) (Decision, error) {
	b, err := os.ReadFile(filepath.Join(c.Dir, ipcFile))
	if err != nil {
		return Decision{}, ErrNoApp
	}
	var ep endpoint
	if json.Unmarshal(b, &ep) != nil || ep.Addr == "" {
		return Decision{}, ErrNoApp
	}
	conn, err := net.DialTimeout("tcp", ep.Addr, 2*time.Second)
	if err != nil {
		return Decision{}, ErrNoApp
	}
	defer conn.Close()
	t := c.Timeout
	if t == 0 {
		t = Timeout
	}
	_ = conn.SetDeadline(time.Now().Add(t + 10*time.Second))
	if err := json.NewEncoder(conn).Encode(wireRequest{Token: ep.Token, Request: r}); err != nil {
		return Decision{}, err
	}
	var d Decision
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&d); err != nil {
		return Decision{}, fmt.Errorf("KeyBar 没有回答：%w", err)
	}
	if d.Why == "bad-token" {
		return Decision{}, ErrNoApp
	}
	return d, nil
}
