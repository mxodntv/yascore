package server

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
)

func (s *Server) skillsWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	ctx := r.Context()
	_ = s.skills.Accept(ctx, wsConn{c: conn})
}

type wsConn struct {
	c *websocket.Conn
}

func (w wsConn) Read(ctx context.Context) ([]byte, error) {
	_, data, err := w.c.Read(ctx)
	return data, err
}

func (w wsConn) Write(ctx context.Context, p []byte) error {
	return w.c.Write(ctx, websocket.MessageText, p)
}

func (w wsConn) Close() error {
	return w.c.Close(websocket.StatusNormalClosure, "")
}
