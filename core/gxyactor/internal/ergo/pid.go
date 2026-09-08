package ergo

import (
	"fmt"
	"strconv"
	"strings"

	"ergo.services/ergo/gen"
	"gserver/core/gxyactor"
)

const RuntimeID = "ergo-v1"

func (a *Adapter) fromErgoPID(pid gen.PID, logicalID string) gxyactor.PID {
	if pid.Node == "" || pid.ID == 0 {
		return gxyactor.PID{}
	}
	if logicalID == "" {
		logicalID = strconv.FormatUint(pid.ID, 10)
	}
	node := string(pid.Node)
	if a != nil && a.transportName != "" && node == a.transportName && a.nodeName != "" {
		node = a.nodeName
	}
	return gxyactor.PID{
		Runtime:  RuntimeID,
		Node:     node,
		ID:       logicalID,
		Creation: strconv.FormatInt(pid.Creation, 10),
	}
}

func (a *Adapter) toErgoPID(pid gxyactor.PID) (gen.PID, error) {
	if pid.IsZero() || pid.Runtime != RuntimeID || pid.Node == "" || pid.ID == "" {
		return gen.PID{}, fmt.Errorf("%w: invalid runtime PID", ErrUnknownPID)
	}
	if pid.Creation == "" {
		return gen.PID{}, fmt.Errorf("%w: missing creation", ErrUnknownPID)
	}
	creation, err := strconv.ParseInt(pid.Creation, 10, 64)
	if err != nil {
		return gen.PID{}, fmt.Errorf("%w: invalid creation", ErrUnknownPID)
	}
	if a != nil && a.nodeName != "" && pid.Node == a.nodeName && a.creation != 0 && creation != a.creation {
		return gen.PID{}, fmt.Errorf("%w: stale creation", ErrUnknownPID)
	}
	if a != nil {
		if raw, ok := a.rawPID(pid); ok {
			if raw.Creation != creation {
				return gen.PID{}, fmt.Errorf("%w: stale PID", ErrUnknownPID)
			}
			return raw, nil
		}
		if a.resolvePID != nil {
			raw, resolveErr := a.resolvePID(pid)
			if resolveErr != nil || raw.ID == 0 || raw.Node == "" || raw.Creation != creation {
				if resolveErr == nil {
					resolveErr = fmt.Errorf("resolver returned invalid or stale PID")
				}
				return gen.PID{}, fmt.Errorf("%w: %v", ErrUnknownPID, resolveErr)
			}
			return raw, nil
		}
	}
	return gen.PID{}, fmt.Errorf("%w: PID is not present in adapter map", ErrUnknownPID)
}
func (a *Adapter) PIDFromErgo(pid gen.PID, logicalID string) gxyactor.PID {
	return a.fromErgoPID(pid, logicalID)
}

func (a *Adapter) PIDToErgo(pid gxyactor.PID) (gen.PID, error) {
	return a.toErgoPID(pid)
}
func namespacedID(kind, id string) string {
	if strings.Contains(id, "/") {
		return id
	}
	if kind == "" {
		return id
	}
	return kind + "/" + id
}
