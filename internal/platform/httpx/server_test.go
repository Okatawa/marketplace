package httpx

import (
 "context"
 "net"
 "net/http"
 "testing"
 "time"
)

func TestRunGracefulShutdown(t *testing.T) {
 listener, err := net.Listen("tcp", "127.0.0.1:0"); if err != nil { t.Fatal(err) }
 entered := make(chan struct{}); release := make(chan struct{})
 ctx, cancel := context.WithCancel(context.Background()); defer cancel()
 done := make(chan error,1)
 go func(){ done <- Serve(ctx, listener, http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){close(entered); <-release; w.Write([]byte("done"))})) }()
 response := make(chan error,1)
 go func(){ resp,err:=http.Get("http://"+listener.Addr().String()); if err==nil {resp.Body.Close()};response<-err }()
 <-entered; cancel()
 select {case <-done:t.Fatal("server did not drain request");case <-time.After(20*time.Millisecond):}
 close(release)
 if err:=<-response;err!=nil {t.Fatal(err)}
 if err:=<-done;err!=nil {t.Fatal(err)}
}
