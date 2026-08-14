package metrics

import (
	"net/http"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func StartMetricsServer(port string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			if logging.Logger != nil {
				logging.Logger.Errorf("Failed to start metrics server: %v", err)
			}
		}
	}()
	return nil
}
