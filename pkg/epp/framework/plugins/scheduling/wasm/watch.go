package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// moduleConfig is the JSON structure stored in the ConfigMap.
type moduleConfig struct {
	Module    string `json:"module"`
	PlainHTTP bool   `json:"plainHTTP,omitempty"`
}

func parseModuleConfig(data string) (moduleConfig, error) {
	var cfg moduleConfig
	if err := json.Unmarshal([]byte(data), &cfg); err != nil {
		return moduleConfig{}, fmt.Errorf("parsing module config: %w", err)
	}
	if cfg.Module == "" {
		return moduleConfig{}, fmt.Errorf("missing 'module' field in config")
	}
	return cfg, nil
}

// configMapRef identifies a ConfigMap and the key within it.
type configMapRef struct {
	Namespace string
	Name      string
	Key       string
}

// watchConfigMap watches a ConfigMap for changes and calls onChange with the
// new module config whenever the data changes.
func watchConfigMap(ctx context.Context, ref configMapRef, onChange func(moduleConfig) error, logger logr.Logger) {
	config, err := rest.InClusterConfig()
	if err != nil {
		logger.Error(err, "failed to get in-cluster config")
		return
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		logger.Error(err, "failed to create kubernetes client")
		return
	}

	logger.Info("watching ConfigMap for wasm plugin config", "namespace", ref.Namespace, "name", ref.Name, "key", ref.Key)
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := doWatch(ctx, clientset, ref, onChange, logger)
		if err != nil && ctx.Err() == nil {
			logger.Error(err, "ConfigMap watch ended, restarting", "backoff", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		} else {
			backoff = time.Second
		}
	}
}

func doWatch(ctx context.Context, clientset kubernetes.Interface, ref configMapRef, onChange func(moduleConfig) error, logger logr.Logger) error {
	watcher, err := clientset.CoreV1().ConfigMaps(ref.Namespace).Watch(ctx, metav1ListOptions(ref.Name))
	if err != nil {
		return fmt.Errorf("starting watch: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("watch channel closed")
			}
			if event.Type == watch.Modified || event.Type == watch.Added {
				cm, ok := event.Object.(*corev1.ConfigMap)
				if !ok {
					continue
				}
				data, exists := cm.Data[ref.Key]
				if !exists {
					logger.Error(nil, "key not found in ConfigMap", "key", ref.Key)
					continue
				}
				cfg, err := parseModuleConfig(data)
				if err != nil {
					logger.Error(err, "failed to parse ConfigMap data")
					continue
				}
				logger.Info("ConfigMap changed, reloading wasm plugin", "module", cfg.Module)
				if err := onChange(cfg); err != nil {
					logger.Error(err, "failed to reload wasm plugin")
				}
			}
		}
	}
}

func metav1ListOptions(name string) metav1.ListOptions {
	return metav1.ListOptions{
		FieldSelector: "metadata.name=" + name,
	}
}
