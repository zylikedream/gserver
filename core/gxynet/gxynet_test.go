package gxynet

import (
	"testing"

	"gserver/core/gxynet/peer"
)

func TestNetwork(t *testing.T) {
	type args struct{}
	tests := []struct {
		name    string
		args    args
		want    peer.Peer
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// got, err := NewNetwork(tt.args.configFile)
			// if (err != nil) != tt.wantErr {
			// 	t.Errorf("NewNetwork() error = %v, wantErr %v", err, tt.wantErr)
			// 	return
			// }
			// if !reflect.DeepEqual(got, tt.want) {
			// 	t.Errorf("NewNetwork() = %v, want %v", got, tt.want)
			// }
		})
	}
}
