package hadock

import (
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"time"
)

type flusher interface {
	Flush() error
}

type proxy struct {
	send chan io.WriteCloser
	recv <-chan io.WriteCloser

	writer io.WriteCloser
}

func DialProxy(addr, level string) (io.WriteCloser, error) {
	if addr == "" {
		return nil, fmt.Errorf("empty address")
	}
	p := proxy{
		send:   make(chan io.WriteCloser),
		writer: Discard(),
	}
	p.recv = reset(p.send, addr, level)
	p.send <- nil
	return &p, nil
}

func (p *proxy) Write(bs []byte) (int, error) {
	select {
	case w, ok := <-p.recv:
		if !ok {
			return 0, io.EOF
		}
		p.writer = w
	default:
	}

	_, err := p.writer.Write(bs)
	f, ok := p.writer.(flusher)
	if err == nil && ok {
		err = f.Flush()
	}

	if _, ok := p.writer.(*discarder); err != nil || ok {
		p.send <- p.writer
		p.writer = Discard()
	}

	return len(bs), nil
}

func (p *proxy) Close() error {
	if f, ok := p.writer.(flusher); ok && p.writer != nil {
		f.Flush()
	}
	select {
	case _, ok := <-p.send:
		if !ok {
			return io.EOF
		}
	default:
	}
	close(p.send)
	return p.writer.Close()
}

func reset(in <-chan io.WriteCloser, addr, level string) <-chan io.WriteCloser {
	queue := make(chan io.WriteCloser)
	go func() {
		defer close(queue)

		sema := make(chan struct{}, 1)
		for {
			wc, ok := <-in
			if !ok {
				return
			}
			select {
			case sema <- struct{}{}:
			default:
				continue
			}
			if wc != nil {
				wc.Close()
			}
			go func() {
				for {
					c, err := net.DialTimeout("tcp", addr, time.Second*5)
					if err != nil {
						time.Sleep(250 * time.Millisecond)
						continue
					}
					var w io.WriteCloser = c

					var lvl int
					deflate := true
					switch level {
					default:
						deflate = false
					case "no":
						lvl = gzip.NoCompression
					case "speed":
						lvl = gzip.BestSpeed
					case "best":
						lvl = gzip.BestCompression
					case "default":
						lvl = gzip.DefaultCompression
					}
					if deflate {
						w, _ = gzip.NewWriterLevel(w, lvl)
					}
					queue <- w
					break
				}
				<-sema
			}()
		}
	}()
	return queue
}

type discarder struct {
	io.Writer
}

func Discard() io.WriteCloser {
	return &discarder{ioutil.Discard}
}

func (_ discarder) Close() error { return nil }
