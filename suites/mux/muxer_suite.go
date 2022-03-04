package test

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"os"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-testing/ci"

	"github.com/stretchr/testify/require"
)

var randomness []byte
var Subtests map[string]TransportTest

func init() {
	// read 1MB of randomness
	randomness = make([]byte, 1<<20)
	if _, err := crand.Read(randomness); err != nil {
		panic(err)
	}

	Subtests = make(map[string]TransportTest)
	for _, f := range subtests {
		Subtests[getFunctionName(f)] = f
	}
}

func getFunctionName(i interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()
}

type Options struct {
	tr        network.Multiplexer
	connNum   int
	streamNum int
	msgNum    int
	msgMin    int
	msgMax    int
}

func randBuf(size int) []byte {
	n := len(randomness) - size
	if size < 1 {
		panic(fmt.Errorf("requested too large buffer (%d). max is %d", size, len(randomness)))
	}

	start := mrand.Intn(n)
	return randomness[start : start+size]
}

func checkErr(t *testing.T, err error) {
	if err != nil {
		debug.PrintStack()
		t.Fatal(err)
	}
}

func log(s string, v ...interface{}) {
	if testing.Verbose() && !ci.IsRunning() {
		fmt.Fprintf(os.Stderr, "> "+s+"\n", v...)
	}
}

func echoStream(s network.MuxedStream) {
	defer s.Close()
	log("accepted stream")
	io.Copy(&LogWriter{s}, s) // echo everything
	log("closing stream")
}

type LogWriter struct {
	W io.Writer
}

func (lw *LogWriter) Write(buf []byte) (int, error) {
	if testing.Verbose() && !ci.IsRunning() {
		log("logwriter: writing %d bytes", len(buf))
	}
	return lw.W.Write(buf)
}

func GoServe(t *testing.T, tr network.Multiplexer, l net.Listener) (done func()) {
	closed := make(chan struct{}, 1)

	go func() {
		for {
			c1, err := l.Accept()
			if err != nil {
				select {
				case <-closed:
					return // closed naturally.
				default:
					checkErr(t, err)
				}
			}

			log("accepted connection")
			sc1, err := tr.NewConn(c1, true, nil)
			checkErr(t, err)
			go func() {
				for {
					str, err := sc1.AcceptStream()
					if err != nil {
						break
					}
					go echoStream(str)
				}
			}()
		}
	}()

	return func() {
		closed <- struct{}{}
	}
}

func SubtestSimpleWrite(t *testing.T, tr network.Multiplexer) {
	l, err := net.Listen("tcp", "localhost:0")
	checkErr(t, err)
	log("listening at %s", l.Addr().String())
	done := GoServe(t, tr, l)
	defer done()

	log("dialing to %s", l.Addr().String())
	nc1, err := net.Dial("tcp", l.Addr().String())
	checkErr(t, err)
	defer nc1.Close()

	log("wrapping conn")
	c1, err := tr.NewConn(nc1, false, nil)
	checkErr(t, err)
	defer c1.Close()

	// serve the outgoing conn, because some muxers assume
	// that we _always_ call serve. (this is an error?)
	go c1.AcceptStream()

	log("creating stream")
	s1, err := c1.OpenStream(context.Background())
	checkErr(t, err)
	defer s1.Close()

	buf1 := randBuf(4096)
	log("writing %d bytes to stream", len(buf1))
	_, err = s1.Write(buf1)
	checkErr(t, err)

	buf2 := make([]byte, len(buf1))
	log("reading %d bytes from stream (echoed)", len(buf2))
	_, err = io.ReadFull(s1, buf2)
	checkErr(t, err)

	require.Equal(t, buf1, buf2)
	log("done")
}

func SubtestStress(t *testing.T, opt Options) {
	msgsize := 1 << 11
	errs := make(chan error) // dont block anything.

	rateLimitN := 5000 // max of 5k funcs, because -race has 8k max.
	rateLimitChan := make(chan struct{}, rateLimitN)
	for i := 0; i < rateLimitN; i++ {
		rateLimitChan <- struct{}{}
	}

	rateLimit := func(f func()) {
		<-rateLimitChan
		f()
		rateLimitChan <- struct{}{}
	}

	writeStream := func(s network.MuxedStream, bufs chan<- []byte) {
		log("writeStream %p, %d msgNum", s, opt.msgNum)

		for i := 0; i < opt.msgNum; i++ {
			buf := randBuf(msgsize)
			bufs <- buf
			log("%p writing %d bytes (message %d/%d #%x)", s, len(buf), i, opt.msgNum, buf[:3])
			if _, err := s.Write(buf); err != nil {
				errs <- fmt.Errorf("s.Write(buf): %s", err)
				continue
			}
		}
	}

	readStream := func(s network.MuxedStream, bufs <-chan []byte) {
		log("readStream %p, %d msgNum", s, opt.msgNum)

		buf2 := make([]byte, msgsize)
		i := 0
		for buf1 := range bufs {
			i++
			log("%p reading %d bytes (message %d/%d #%x)", s, len(buf1), i-1, opt.msgNum, buf1[:3])

			if _, err := io.ReadFull(s, buf2); err != nil {
				errs <- fmt.Errorf("io.ReadFull(s, buf2): %s", err)
				log("%p failed to read %d bytes (message %d/%d #%x)", s, len(buf1), i-1, opt.msgNum, buf1[:3])
				continue
			}
			if !bytes.Equal(buf1, buf2) {
				errs <- fmt.Errorf("buffers not equal (%x != %x)", buf1[:3], buf2[:3])
			}
		}
	}

	openStreamAndRW := func(c network.MuxedConn) {
		log("openStreamAndRW %p, %d opt.msgNum", c, opt.msgNum)

		s, err := c.OpenStream(context.Background())
		if err != nil {
			errs <- fmt.Errorf("failed to create NewStream: %s", err)
			return
		}

		bufs := make(chan []byte, opt.msgNum)
		go func() {
			writeStream(s, bufs)
			close(bufs)
		}()

		readStream(s, bufs)
		s.Close()
	}

	openConnAndRW := func() {
		log("openConnAndRW")

		l, err := net.Listen("tcp", "localhost:0")
		checkErr(t, err)
		done := GoServe(t, opt.tr, l)
		defer done()

		nla := l.Addr()
		nc, err := net.Dial(nla.Network(), nla.String())
		checkErr(t, err)
		if err != nil {
			t.Fatal(fmt.Errorf("net.Dial(%s, %s): %s", nla.Network(), nla.String(), err))
			return
		}

		c, err := opt.tr.NewConn(nc, false, nil)
		if err != nil {
			t.Fatal(fmt.Errorf("a.AddConn(%s <--> %s): %s", nc.LocalAddr(), nc.RemoteAddr(), err))
			return
		}

		// serve the outgoing conn, because some muxers assume
		// that we _always_ call serve. (this is an error?)
		go func() {
			log("serving connection")
			for {
				str, err := c.AcceptStream()
				if err != nil {
					break
				}
				go echoStream(str)
			}
		}()

		var wg sync.WaitGroup
		for i := 0; i < opt.streamNum; i++ {
			wg.Add(1)
			go rateLimit(func() {
				defer wg.Done()
				openStreamAndRW(c)
			})
		}
		wg.Wait()
		c.Close()
	}

	openConnsAndRW := func() {
		log("openConnsAndRW, %d conns", opt.connNum)

		var wg sync.WaitGroup
		for i := 0; i < opt.connNum; i++ {
			wg.Add(1)
			go rateLimit(func() {
				defer wg.Done()
				openConnAndRW()
			})
		}
		wg.Wait()
	}

	go func() {
		openConnsAndRW()
		close(errs) // done
	}()

	for err := range errs {
		t.Error(err)
	}

}

func tcpPipe(t *testing.T) (net.Conn, net.Conn) {
	list, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}

	con1, err := net.Dial("tcp", list.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	con2, err := list.Accept()
	if err != nil {
		t.Fatal(err)
	}

	return con1, con2
}

func SubtestStreamOpenStress(t *testing.T, tr network.Multiplexer) {
	wg := new(sync.WaitGroup)

	a, b := tcpPipe(t)
	defer a.Close()
	defer b.Close()

	defer wg.Wait()

	wg.Add(1)
	count := 10000
	workers := 5
	go func() {
		defer wg.Done()
		muxa, err := tr.NewConn(a, true, nil)
		if err != nil {
			t.Error(err)
			return
		}
		stress := func() {
			defer wg.Done()
			for i := 0; i < count; i++ {
				s, err := muxa.OpenStream(context.Background())
				if err != nil {
					t.Error(err)
					return
				}
				err = s.CloseWrite()
				if err != nil {
					t.Error(err)
				}
				n, err := s.Read([]byte{0})
				if n != 0 {
					t.Error("expected to read no bytes")
				}
				if err != io.EOF {
					t.Errorf("expected an EOF, got %s", err)
				}
			}
		}

		for i := 0; i < workers; i++ {
			wg.Add(1)
			go stress()
		}
	}()

	muxb, err := tr.NewConn(b, false, nil)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Millisecond * 50)

	wg.Add(1)
	recv := make(chan struct{}, count*workers)
	go func() {
		defer wg.Done()
		for i := 0; i < count*workers; i++ {
			str, err := muxb.AcceptStream()
			if err != nil {
				break
			}
			go func() {
				str.Close()
				select {
				case recv <- struct{}{}:
				default:
					t.Error("too many stream")
				}
			}()
		}
	}()

	timeout := time.Second * 10
	if ci.IsRunning() {
		timeout *= 10
	}

	limit := time.After(timeout)
	for i := 0; i < count*workers; i++ {
		select {
		case <-recv:
		case <-limit:
			t.Fatal("timed out receiving streams")
		}
	}
}

func SubtestStreamReset(t *testing.T, tr network.Multiplexer) {
	wg := new(sync.WaitGroup)
	defer wg.Wait()

	a, b := tcpPipe(t)
	defer a.Close()
	defer b.Close()

	wg.Add(1)
	go func() {
		defer wg.Done()
		muxa, err := tr.NewConn(a, true, nil)
		if err != nil {
			t.Error(err)
			return
		}
		s, err := muxa.OpenStream(context.Background())
		if err != nil {
			t.Error(err)
			return
		}
		time.Sleep(time.Millisecond * 50)

		_, err = s.Write([]byte("foo"))
		if err != network.ErrReset {
			t.Error("should have been stream reset")
		}

		s.Close()
	}()

	muxb, err := tr.NewConn(b, false, nil)
	if err != nil {
		t.Fatal(err)
	}

	str, err := muxb.AcceptStream()
	checkErr(t, err)
	str.Reset()

	wg.Wait()
}

// check that Close also closes the underlying net.Conn
func SubtestWriteAfterClose(t *testing.T, tr network.Multiplexer) {
	a, b := tcpPipe(t)

	muxa, err := tr.NewConn(a, true, nil)
	checkErr(t, err)

	muxb, err := tr.NewConn(b, false, nil)
	checkErr(t, err)

	err = muxa.Close()
	checkErr(t, err)
	err = muxb.Close()
	checkErr(t, err)

	// make sure the underlying net.Conn was closed
	if _, err := a.Write([]byte("foobar")); err == nil || !strings.Contains(err.Error(), "use of closed network connection") {
		t.Fatal("write should have failed")
	}
	if _, err := b.Write([]byte("foobar")); err == nil || !strings.Contains(err.Error(), "use of closed network connection") {
		t.Fatal("write should have failed")
	}
}

func SubtestStress1Conn1Stream1Msg(t *testing.T, tr network.Multiplexer) {
	SubtestStress(t, Options{
		tr:        tr,
		connNum:   1,
		streamNum: 1,
		msgNum:    1,
		msgMax:    100,
		msgMin:    100,
	})
}

func SubtestStress1Conn1Stream100Msg(t *testing.T, tr network.Multiplexer) {
	SubtestStress(t, Options{
		tr:        tr,
		connNum:   1,
		streamNum: 1,
		msgNum:    100,
		msgMax:    100,
		msgMin:    100,
	})
}

func SubtestStress1Conn100Stream100Msg(t *testing.T, tr network.Multiplexer) {
	SubtestStress(t, Options{
		tr:        tr,
		connNum:   1,
		streamNum: 100,
		msgNum:    100,
		msgMax:    100,
		msgMin:    100,
	})
}

func SubtestStress10Conn10Stream50Msg(t *testing.T, tr network.Multiplexer) {
	SubtestStress(t, Options{
		tr:        tr,
		connNum:   10,
		streamNum: 10,
		msgNum:    50,
		msgMax:    100,
		msgMin:    100,
	})
}

func SubtestStress1Conn1000Stream10Msg(t *testing.T, tr network.Multiplexer) {
	SubtestStress(t, Options{
		tr:        tr,
		connNum:   1,
		streamNum: 1000,
		msgNum:    10,
		msgMax:    100,
		msgMin:    100,
	})
}

func SubtestStress1Conn100Stream100Msg10MB(t *testing.T, tr network.Multiplexer) {
	SubtestStress(t, Options{
		tr:        tr,
		connNum:   1,
		streamNum: 100,
		msgNum:    100,
		msgMax:    10000,
		msgMin:    1000,
	})
}

// Subtests are all the subtests run by SubtestAll
var subtests = []TransportTest{
	SubtestSimpleWrite,
	SubtestWriteAfterClose,
	SubtestStress1Conn1Stream1Msg,
	SubtestStress1Conn1Stream100Msg,
	SubtestStress1Conn100Stream100Msg,
	SubtestStress10Conn10Stream50Msg,
	SubtestStress1Conn1000Stream10Msg,
	SubtestStress1Conn100Stream100Msg10MB,
	SubtestStreamOpenStress,
	SubtestStreamReset,
}

// SubtestAll runs all the stream multiplexer tests against the target
// transport.
func SubtestAll(t *testing.T, tr network.Multiplexer) {
	for name, f := range Subtests {
		t.Run(name, func(t *testing.T) {
			f(t, tr)
		})
	}
}

// TransportTest is a stream multiplex transport test case
type TransportTest func(t *testing.T, tr network.Multiplexer)
