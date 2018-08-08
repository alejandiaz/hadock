package hadock

import (
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"time"
	"sync"
)

type flusher interface {
	Flush() error
}

type proxy struct {
	level  string

	mu sync.Mutex
	net.Conn
	inner io.Writer
}

func DialProxy(addr, level string) (io.WriteCloser, error) {
	if addr == "" {
		return nil, fmt.Errorf("empty address")
	}
	p := proxy{inner: ioutil.Discard, level: level}
	go p.reset(addr)
	return &p, nil
}

func (p *proxy) Write(bs []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, err := p.inner.Write(bs)
  f, ok := p.inner.(flusher)
  if err == nil && ok {
    err = f.Flush()
  }

	if err != nil {
		p.inner = ioutil.Discard
		p.Conn.Close()
		go p.reset(p.Conn.RemoteAddr().String())
	}

	return len(bs), nil
}

func (p *proxy) Close() error {
	if p.Conn == nil {
		return nil
	}
  if f, ok := p.inner.(flusher); ok {
    f.Flush()
  }
	return p.Conn.Close()
}

func (p *proxy) reset(addr string) {
	p.inner = ioutil.Discard
	for {
		c, err := net.DialTimeout("tcp", addr, time.Second*5)
		if err == nil {
			p.mu.Lock()
			defer p.mu.Unlock()

			p.Conn, p.inner = c, c

			var level int
			deflate := true
			switch p.level {
			default:
				deflate = false
			case "no":
				level = gzip.NoCompression
			case "speed":
				level = gzip.BestSpeed
			case "best":
				level = gzip.BestCompression
			case "default":
				level = gzip.DefaultCompression
			}
      if deflate {
        p.inner, _ = gzip.NewWriterLevel(p.inner, level)
      }
			return
		}
		time.Sleep(time.Millisecond * 100)
	}
}
