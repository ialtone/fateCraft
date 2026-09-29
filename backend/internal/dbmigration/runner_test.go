package dbmigration_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ialtone/fatecraft/internal/dbmigration"
)

// A server that accepts TCP but never completes PostgreSQL startup must not
// defeat the connection deadline. This does not require a real database.
func TestOpenHonorsStartupDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		select {
		case accepted <- conn:
		case <-stop:
			return
		}
		<-stop
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		db, err := dbmigration.Open(ctx, "postgres://test:test@"+listener.Addr().String()+"/test?sslmode=disable")
		if db != nil {
			_ = db.Close()
		}
		result <- err
	}()
	select {
	case <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("migration connection did not reach the test listener")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Open = %v, want deadline exceeded during startup", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Open ignored the connection deadline")
	}
}
