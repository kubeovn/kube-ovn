// Package kohelper implements the private, versioned helper protocol. A gRPC
// connection is carried over isolated helper stdin/stdout, never a listening node port.
package kohelper

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	Version = 1
	method  = "/kubeovn.ko.Helper/Run"
)

// Request is validated before an operation starts. Pod identity fields are
// reserved for operations that need an explicit workload namespace context.
type Request struct {
	Version     int      `json:"version"`
	Argv        []string `json:"argv"`
	Namespace   string   `json:"namespace,omitempty"`
	Pod         string   `json:"pod,omitempty"`
	UID         string   `json:"uid,omitempty"`
	Netns       string   `json:"netns,omitempty"`
	HostNetwork bool     `json:"hostNetwork,omitzero"`
}

type Result struct {
	Code  int    `json:"code"`
	Error string `json:"error,omitempty"`
}

// ExitError preserves a tool's status across the helper protocol.
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string   { return e.Message }
func (e *ExitError) ExitStatus() int { return e.Code }
func (e *ExitError) Exited() bool    { return true }
func (e *ExitError) String() string  { return e.Error() }

// StreamConn adapts helper byte pipes to the single gRPC connection. The
// transport callback must close both pipes on cancellation or stream failure.
type StreamConn struct {
	Reader io.ReadCloser
	Writer io.WriteCloser
}

func (c *StreamConn) Read(p []byte) (int, error)     { return c.Reader.Read(p) }
func (c *StreamConn) Write(p []byte) (int, error)    { return c.Writer.Write(p) }
func (c *StreamConn) Close() error                   { return errors.Join(c.Reader.Close(), c.Writer.Close()) }
func (*StreamConn) LocalAddr() net.Addr              { return streamAddr("local") }
func (*StreamConn) RemoteAddr() net.Addr             { return streamAddr("remote") }
func (*StreamConn) SetDeadline(time.Time) error      { return nil }
func (*StreamConn) SetReadDeadline(time.Time) error  { return nil }
func (*StreamConn) SetWriteDeadline(time.Time) error { return nil }

type streamAddr string

func (a streamAddr) Network() string { return "attach" }
func (a streamAddr) String() string  { return string(a) }

// Dial never reconnects or repeats an operation after a transport failure.
func Dial(connection net.Conn) (*grpc.ClientConn, error) {
	var mu sync.Mutex
	used := false
	return grpc.NewClient("passthrough:///attach", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			mu.Lock()
			defer mu.Unlock()
			if used {
				return nil, errors.New("attach transport cannot reconnect")
			}
			used = true
			return connection, nil
		}),
		grpc.WithDisableRetry())
}

// Run sends one request and requires a final status frame, even for exit zero.
func Run(ctx context.Context, conn *grpc.ClientConn, request Request, stdout, stderr io.Writer) error {
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := conn.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, method)
	if err != nil {
		return err
	}
	if err = stream.SendMsg(wrapperspb.Bytes(data)); err != nil {
		return err
	}
	if err = stream.CloseSend(); err != nil {
		return err
	}
	finished := false
	for {
		frame := new(wrapperspb.BytesValue)
		if err = stream.RecvMsg(frame); err != nil {
			if errors.Is(err, io.EOF) && finished {
				return nil
			}
			return fmt.Errorf("helper stream ended without success: %w", err)
		}
		if finished || len(frame.Value) == 0 {
			return errors.New("invalid helper frame order")
		}
		payload := frame.Value[1:]
		switch frame.Value[0] {
		case 1, 2:
			writer := stdout
			if frame.Value[0] == 2 {
				writer = stderr
			}
			if writer != nil {
				if _, err = writer.Write(payload); err != nil {
					return err
				}
			}
		case 3:
			var result Result
			if err = json.Unmarshal(payload, &result); err != nil {
				return err
			}
			finished = true
			if result.Code != 0 || result.Error != "" {
				return &ExitError{Code: max(1, result.Code), Message: result.Error}
			}
		default:
			return errors.New("unknown helper frame type")
		}
	}
}

// Runner receives only validated helper requests; it must terminate the command
// process group when the RPC context is cancelled, and keep stderr separate from binary stdout.
type Runner interface {
	Run(context.Context, Request, io.Writer, io.Writer) Result
}
type (
	service interface{ serve(grpc.ServerStream) error }
	server  struct{ runner Runner }
)

func (s *server) serve(stream grpc.ServerStream) error {
	envelope := new(wrapperspb.BytesValue)
	if err := stream.RecvMsg(envelope); err != nil {
		return err
	}
	var request Request
	if err := json.Unmarshal(envelope.Value, &request); err != nil {
		return err
	}
	if request.Version != Version {
		return errors.New("unsupported helper protocol version")
	}
	frames := &frameWriter{stream: stream}
	result := s.runner.Run(stream.Context(), request, &channelWriter{frames, 1}, &channelWriter{frames, 2})
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return frames.send(append([]byte{3}, payload...))
}

type frameWriter struct {
	mu     sync.Mutex
	stream grpc.ServerStream
}

func (w *frameWriter) send(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stream.SendMsg(wrapperspb.Bytes(data))
}

type channelWriter struct {
	frames  *frameWriter
	channel byte
}

func (w *channelWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n := min(len(p), 32<<10)
		frame := append([]byte{w.channel}, p[:n]...)
		if err := w.frames.send(frame); err != nil {
			return total, err
		}
		total += n
		p = p[n:]
	}
	return total, nil
}

// Serve accepts exactly one connection; closing it ends the helper process.
func Serve(ctx context.Context, connection net.Conn, runner Runner) error {
	srv := grpc.NewServer(grpc.MaxRecvMsgSize(64<<10), grpc.MaxConcurrentStreams(16), grpc.WaitForHandlers(true))
	// A closed transport ends Serve before RPC handlers finish cancelling their
	// commands. Wait for process-group cleanup before the helper can exit.
	defer srv.Stop()
	srv.RegisterService(&grpc.ServiceDesc{ServiceName: "kubeovn.ko.Helper", HandlerType: (*service)(nil), Streams: []grpc.StreamDesc{{StreamName: "Run", ServerStreams: true, Handler: func(s any, stream grpc.ServerStream) error { return s.(service).serve(stream) }}}}, &server{runner})
	listener := &oneListener{conn: connection, done: make(chan struct{})}
	stop := context.AfterFunc(ctx, func() { srv.Stop(); _ = listener.Close() })
	defer stop()
	err := srv.Serve(listener)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

type oneListener struct {
	mu   sync.Mutex
	conn net.Conn
	done chan struct{}
	once sync.Once
}

func (l *oneListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	c := l.conn
	l.conn = nil
	l.mu.Unlock()
	if c != nil {
		return &trackedConn{Conn: c, close: func() { _ = l.Close() }}, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *oneListener) Close() error { l.once.Do(func() { close(l.done) }); return nil }
func (*oneListener) Addr() net.Addr { return streamAddr("helper") }

type trackedConn struct {
	net.Conn
	close func()
}

func (c *trackedConn) Close() error { err := c.Conn.Close(); c.close(); return err }
