package friend

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"gserver/core/gxypgx"
)

type FriendServer struct {
	server *http.Server
	cfg    *Config
}

func NewFriendServer(addr string) *FriendServer {
	cfg := LoadConfig()
	return &FriendServer{
		cfg: cfg,
		server: &http.Server{
			Addr:    addr,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handleRequest(w, r, cfg)
			}),
		},
	}
}

func (s *FriendServer) Start() error {
	return s.server.ListenAndServe()
}

func (s *FriendServer) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func handleRequest(w http.ResponseWriter, r *http.Request, cfg *Config) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path
	ctx := r.Context()

	var err error
	switch path {
	case "/friend/send_request":
		err = handleSendRequest(ctx, w, r, cfg)
	case "/friend/accept_request":
		err = handleAcceptRequest(ctx, w, r, cfg)
	case "/friend/reject_request":
		err = handleRejectRequest(ctx, w, r, cfg)
	case "/friend/remove_friend":
		err = handleRemoveFriend(ctx, w, r, cfg)
	case "/friend/list":
		err = handleFriendList(ctx, w, r)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, err)
	}
}

func handleSendRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, cfg *Config) error {
	fromID, toID, err := parseTwoIDs(r)
	if err != nil {
		return err
	}
	err = SendRequest(ctx, fromID, toID, cfg)
	if err != nil {
		return err
	}
	return writeOK(w)
}

func handleAcceptRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, cfg *Config) error {
	myID, fromID, err := parseTwoIDs(r)
	if err != nil {
		return err
	}
	err = AcceptRequest(ctx, myID, fromID, cfg)
	if err != nil {
		return err
	}
	return writeOK(w)
}

func handleRejectRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, cfg *Config) error {
	myID, fromID, err := parseTwoIDs(r)
	if err != nil {
		return err
	}
	tx := openTx(ctx)
	defer tx.Rollback()

	a, b, err := lockBoth(tx, myID, fromID)
	if err != nil {
		return err
	}
	if a.PlayerID != myID {
		a, b = b, a
	}
	me, other := a, b

	if !me.Incoming.Has(fromID) {
		return ErrApplyNotFound
	}
	me.Incoming = removeFromSlice(me.Incoming, fromID)
	other.Outgoing = removeFromSlice(other.Outgoing, myID)

	if err := saveRow(tx, me); err != nil {
		return err
	}
	if err := saveRow(tx, other); err != nil {
		return err
	}
	return writeOK(w, tx.Commit().Error)
}

func handleRemoveFriend(ctx context.Context, w http.ResponseWriter, r *http.Request, cfg *Config) error {
	myID, targetID, err := parseTwoIDs(r)
	if err != nil {
		return err
	}
	err = RemoveFriend(ctx, myID, targetID, cfg)
	if err != nil {
		return err
	}
	return writeOK(w)
}

func handleFriendList(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	playerID, err := parsePlayerID(r)
	if err != nil {
		return err
	}
	var data FriendData
	err = gxypgx.DB().WithContext(ctx).First(&data, playerID).Error
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(map[string]interface{}{
		"code": 0,
		"data": data,
	})
}

// ---- HTTP helpers ----

type httpResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func writeOK(w http.ResponseWriter, err ...error) error {
	if len(err) > 0 && err[0] != nil {
		return err[0]
	}
	return json.NewEncoder(w).Encode(httpResponse{Code: 0})
}

func writeError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(httpResponse{
		Code:    1,
		Message: err.Error(),
	})
}

func parsePlayerID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.URL.Query().Get("player_id"), 10, 64)
}

func parseTwoIDs(r *http.Request) (int64, int64, error) {
	a, err := strconv.ParseInt(r.URL.Query().Get("a"), 10, 64)
	if err != nil {
		return 0, 0, err
	}
	b, err := strconv.ParseInt(r.URL.Query().Get("b"), 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return a, b, nil
}
