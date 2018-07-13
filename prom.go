package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/api/core/v1"
)

const (
	defaultInterval = 30
	defaultPort     = ":9297"
)

const rootDoc = `<html>
<head><title>kube-event-watcher metrics</title></head>
<body>
<h1>kube-event-watcher metrics</h1>
<p><a href="/metrics">Metrics</a></p>
</body>
</html>
`

var addr = flag.String("listen-address", defaultPort, "The address to listen on for HTTP requests.")
var interval = flag.Int("interval", defaultInterval, "Interval to scrape HPA status.")

var labels = []string{
	"ref_namespace",
	"ref_kind",
	"ref_fieldpath",
	"ref_name",
	"reason",
	"message",
	"type",
}

var (
	eventWatcherEventCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ew_event_count",
			Help: "Number of events.",
		},
		labels,
	)
)

func init() {
	prometheus.MustRegister(eventWatcherEventCount)
}

func promServer() {
	flag.Parse()
	fmt.Println("start kube-event-watcher metrics exporter")
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(rootDoc))
		})

		fmt.Println("error :", http.ListenAndServe(*addr, nil))
	}()
}

func setPromMetrics(obj interface{}) {
	e := obj.(*v1.Event)
	label := prometheus.Labels{
		"ref_namespace": e.ObjectMeta.Namespace,
		"ref_kind":      e.InvolvedObject.Kind,
		"ref_fieldpath": e.InvolvedObject.FieldPath,
		"ref_name":      e.InvolvedObject.Name,
		"reason":        e.Reason,
		"message":       e.Message,
		"type":          e.Type,
	}
	eventWatcherEventCount.With(label).Set(float64(e.Count))
}
