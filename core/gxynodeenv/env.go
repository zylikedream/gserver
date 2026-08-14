package gxynodeenv

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"gserver/core/gxyregistery"

	"github.com/cockroachdb/errors"
)

const (
	serviceAccountNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	serviceAccountTokenPath     = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	serviceAccountCAPath        = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

type NodeEnv interface {
	State(ctx context.Context) (gxyregistery.ServiceState, error)
}

type LocalNodeEnv struct{}

func NewLocalNodeEnv() *LocalNodeEnv {
	return &LocalNodeEnv{}
}

func (LocalNodeEnv) State(context.Context) (gxyregistery.ServiceState, error) {
	return gxyregistery.ServiceStateServing, nil
}

func NewAutoNodeEnv() NodeEnv {
	gsName := os.Getenv("GS_NAME")
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	if gsName == "" || host == "" {
		return NewLocalNodeEnv()
	}
	env, err := NewK8sNodeEnvFromEnv()
	if err != nil {
		return NewLocalNodeEnv()
	}
	return env
}

type K8sNodeEnv struct {
	client    *http.Client
	apiServer string
	namespace string
	gsName    string
	token     string
}

func NewK8sNodeEnvFromEnv() (*K8sNodeEnv, error) {
	gsName := os.Getenv("GS_NAME")
	if gsName == "" {
		return nil, errors.Newf("GS_NAME is empty")
	}
	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		data, err := os.ReadFile(serviceAccountNamespacePath)
		if err != nil {
			return nil, errors.Wrap(err, "read namespace")
		}
		namespace = strings.TrimSpace(string(data))
	}
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if port == "" {
		port = "443"
	}
	if host == "" {
		return nil, errors.Newf("KUBERNETES_SERVICE_HOST is empty")
	}
	tokenData, err := os.ReadFile(serviceAccountTokenPath)
	if err != nil {
		return nil, errors.Wrap(err, "read service account token")
	}
	client, err := newK8sHTTPClient()
	if err != nil {
		return nil, err
	}
	return &K8sNodeEnv{
		client:    client,
		apiServer: "https://" + host + ":" + port,
		namespace: namespace,
		gsName:    gsName,
		token:     strings.TrimSpace(string(tokenData)),
	}, nil
}

func newK8sHTTPClient() (*http.Client, error) {
	caData, err := os.ReadFile(serviceAccountCAPath)
	if err != nil {
		return nil, errors.Wrap(err, "read service account ca")
	}
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(caData); !ok {
		return nil, errors.Newf("append service account ca")
	}
	return &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
	}, nil
}

func (e *K8sNodeEnv) State(ctx context.Context) (gxyregistery.ServiceState, error) {
	url := fmt.Sprintf("%s/apis/game.kruise.io/v1alpha1/namespaces/%s/gameservers/%s", e.apiServer, e.namespace, e.gsName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := e.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.Newf("get gameserver %s/%s: %s", e.namespace, e.gsName, resp.Status)
	}
	var gs struct {
		Spec struct {
			OpsState string `json:"opsState"`
		} `json:"spec"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gs); err != nil {
		return "", err
	}
	return mapOKGOpsState(gs.Spec.OpsState), nil
}

func mapOKGOpsState(opsState string) gxyregistery.ServiceState {
	switch opsState {
	case "WaitToBeDeleted", "Kill":
		return gxyregistery.ServiceStateDraining
	case "Maintaining":
		return gxyregistery.ServiceStateMaintaining
	default:
		return gxyregistery.ServiceStateServing
	}
}
