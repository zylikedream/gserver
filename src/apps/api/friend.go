package api

type FriendBatchItem struct {
	TargetID int64  `json:"target_id"`
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
}
