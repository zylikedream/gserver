package lib

import (
	"context"
	"fmt"
	"gserver/core/gxyhttp"
	"net/url"
)

func SendSystemMsg(ctx context.Context, content string) error {
	_, err := gxyhttp.HttpSystem().PostService(ctx, "chat-http",
		fmt.Sprintf("store_system?content=%s", url.QueryEscape(content)))
	return err
}
