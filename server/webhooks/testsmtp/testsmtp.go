// Package testsmtp is the mock mail server the channel tests send through.
//
// It speaks SMTP: greeting, EHLO, STARTTLS, AUTH, MAIL FROM, RCPT TO, DATA,
// QUIT. That is the point of it. The layer under test is OurWay's SMTP client,
// so the fixture is a server, not a stub of the client — the same standard 2.4-0
// sets for the identity provider, and the reason a mock `net/smtp` would be
// worthless here.
//
// Modes cover the outcomes a mail server produces in a real installation:
// accepting, refusing a recipient, refusing the message, requiring
// authentication, requiring encryption, and offering STARTTLS and then breaking
// the handshake.
package testsmtp

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"
)

// Message is one accepted message, as the server saw it.
type Message struct {
	From string
	To   []string
	Data string
}

// Server is a mock SMTP server on a loopback port.
type Server struct {
	listener net.Listener
	addr     string

	mu          sync.Mutex
	messages    []Message
	acceptAll   bool
	rejectTo    map[string]bool
	rejectData  bool
	requireAuth bool
	authUser    string
	authPass    string
	startTLSMode
	cert tls.Certificate

	stop chan struct{}
	done sync.WaitGroup
}

type startTLSMode int

const (
	// startTLSNone does not advertise STARTTLS at all: a client configured with
	// SMTP_STARTTLS refuses the connection rather than sending credentials in clear.
	startTLSNone startTLSMode = iota
	// startTLSWorks advertises and completes STARTTLS.
	startTLSWorks
	// startTLSBroken advertises STARTTLS and drops the connection when the
	// client asks for it: the handshake fails, and the client must say so.
	startTLSBroken
)

// Option configures a Server.
type Option func(*Server)

// RequireAuth makes the server advertise AUTH and refuse a client that does not
// authenticate with these credentials.
func RequireAuth(user, password string) Option {
	return func(s *Server) { s.requireAuth, s.authUser, s.authPass = true, user, password }
}

// WithStartTLS selects the STARTTLS behaviour (default: works).
func WithStartTLS(mode string) Option {
	return func(s *Server) {
		switch mode {
		case "none":
			s.startTLSMode = startTLSNone
		case "broken":
			s.startTLSMode = startTLSBroken
		default:
			s.startTLSMode = startTLSWorks
		}
	}
}

// RejectRecipient refuses RCPT TO for this address with a 550.
func RejectRecipient(address string) Option {
	return func(s *Server) { s.rejectTo[address] = true }
}

// RejectData refuses the message body with a 552 after DATA.
func RejectData() Option {
	return func(s *Server) { s.rejectData = true }
}

// New starts a server. The default accepts everything.
func New(opts ...Option) (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	server := &Server{
		listener:     listener,
		addr:         listener.Addr().String(),
		acceptAll:    true,
		rejectTo:     map[string]bool{},
		startTLSMode: startTLSWorks,
		stop:         make(chan struct{}),
	}
	for _, opt := range opts {
		opt(server)
	}
	if server.cert, err = selfSignedCert(); err != nil {
		listener.Close()
		return nil, err
	}
	server.done.Add(1)
	go server.acceptLoop()
	return server, nil
}

// Host is the address a client dials.
func (s *Server) Host() string { return "127.0.0.1" }

// Port is the TCP port it dials.
func (s *Server) Port() int {
	_, port, err := net.SplitHostPort(s.addr)
	if err != nil {
		return 0
	}
	var n int
	fmt.Sscanf(port, "%d", &n)
	return n
}

// Close stops the server.
func (s *Server) Close() {
	close(s.stop)
	s.listener.Close()
	s.done.Wait()
}

// Messages returns the messages the server accepted, in arrival order.
func (s *Server) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Message, len(s.messages))
	copy(out, s.messages)
	return out
}

// Reset clears recorded messages.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = nil
}

func (s *Server) acceptLoop() {
	defer s.done.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stop:
				return
			default:
				return
			}
		}
		go s.session(conn)
	}
}

func (s *Server) session(conn net.Conn) {
	defer conn.Close()
	remote := newReader(conn)
	send := func(line string) { remote.WriteString(line + "\r\n") }

	send("220 testsmtp ESMTP OurWay test fixture")
	var (
		from       string
		recipients []string
	)
	for {
		line, err := remote.ReadLine()
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
		verb := strings.ToUpper(strings.Fields(line)[0])
		switch verb {
		case "EHLO", "HELO":
			extensions := []string{"250-testsmtp GREETS YOU"}
			if s.startTLSMode != startTLSNone {
				extensions = append(extensions, "250-STARTTLS")
			}
			if s.requireAuth {
				extensions = append(extensions, "250-AUTH PLAIN LOGIN")
			}
			extensions = append(extensions, "250 SIZE 10485760")
			for _, ext := range extensions {
				send(ext)
			}
		case "STARTTLS":
			if s.startTLSMode == startTLSNone {
				send("502 not implemented")
				continue
			}
			send("220 ready to start tls")
			if s.startTLSMode == startTLSBroken {
				// The server "goes encrypted" and vanishes: the client's handshake
				// fails, which is what a broken STARTTLS looks like from a client.
				return
			}
			server := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.cert}})
			if err := server.Handshake(); err != nil {
				return
			}
			conn = server
			remote = newReader(conn)
		case "AUTH":
			if !s.requireAuth {
				send("503 credentials already supplied")
				continue
			}
			if decodeAuth(line) == s.authUser {
				send("235 authenticated")
				continue
			}
			send("535 5.7.8 authentication failed")
		case "MAIL":
			from = extractAddress(line)
			send("250 OK")
		case "RCPT":
			address := extractAddress(line)
			s.mu.Lock()
			rejected := s.rejectTo[address]
			s.mu.Unlock()
			if rejected {
				send("550 5.1.1 recipient refused")
				continue
			}
			recipients = append(recipients, address)
			send("250 OK")
		case "DATA":
			send("354 send it")
			body := readData(remote)
			if s.rejectData {
				send("552 5.3.4 message refused")
				continue
			}
			s.mu.Lock()
			s.messages = append(s.messages, Message{From: from, To: recipients, Data: body})
			s.mu.Unlock()
			send("250 2.0.0 accepted")
		case "RSET":
			from, recipients = "", nil
			send("250 OK")
		case "NOOP":
			send("250 OK")
		case "QUIT":
			send("221 bye")
			return
		default:
			send("250 OK")
		}
	}
}

// helpers ------------------------------------------------------------------

// lineReader is the line-oriented view of the connection SMTP speaks over.
type lineReader struct {
	conn   net.Conn
	reader *bufio.Reader
}

func newReader(conn net.Conn) *lineReader {
	return &lineReader{conn: conn, reader: bufio.NewReader(conn)}
}

func (r *lineReader) ReadLine() (string, error) {
	line, err := r.reader.ReadString('\n')
	if err != nil {
		return line, err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (r *lineReader) WriteString(text string) {
	_, _ = r.conn.Write([]byte(text))
}

// readData reads the message body up to the terminating dot.
func readData(r *lineReader) string {
	var body strings.Builder
	for {
		line, err := r.ReadLine()
		if err != nil {
			break
		}
		if line == "." {
			break
		}
		// A leading dot is stuffed per RFC 5321 (".." means ".").
		body.WriteString(strings.TrimPrefix(line, "."))
		body.WriteString("\n")
	}
	return body.String()
}

// extractAddress pulls the address out of MAIL FROM:<x> / RCPT TO:<x>.
func extractAddress(line string) string {
	start := strings.Index(line, "<")
	end := strings.Index(line, ">")
	if start >= 0 && end > start {
		return line[start+1 : end]
	}
	parts := strings.SplitN(line, ":", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(line)
}

// decodeAuth decodes an `AUTH PLAIN <base64>` token: \0user\0password.
func decodeAuth(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(fields[2])
	if err != nil {
		return ""
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

func selfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

var _ = log.Print
