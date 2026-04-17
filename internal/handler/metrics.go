package handler

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "logiflow_http_requests_total",
		Help: "Количество HTTP запросов",
	}, []string{"method", "path", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "logiflow_http_duration_seconds",
		Help:    "Время обработки HTTP запросов",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})
)

func (s *Server) MiddlewareMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrapped := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		httpRequests.WithLabelValues(
			r.Method,
			r.URL.Path,
			strconv.Itoa(wrapped.status),
		).Inc()

		httpDuration.WithLabelValues(r.Method, r.URL.Path).
			Observe(time.Since(start).Seconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijack not supportd")
	}
	return h.Hijack()
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
