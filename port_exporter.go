package main

import (
    "flag"
    "strings"
    "strconv"
    "sync"
    "log"
    "time"
    "net"
    "net/http"

    "github.com/prometheus/client_golang/prometheus"
)

const (
    namespace = "portprobe"
)

var (
    listenAddress = flag.String("web.listen-address", ":9105", "Address to listen on for web interface and telemetry.")
    metricPath    = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
    probe         = flag.String("probe", ":80,127.0.0.1:22", "Port list for probing")
    status        = flag.String("status", "0", "Default value for probe")
)

type Exporter struct {
    mutex              sync.RWMutex
    duration,error     prometheus.Gauge
    totalScrapes       prometheus.Counter
    metrics            map[string]prometheus.Gauge
}

// return new empty exporter
func NewPortProbeExporter() *Exporter {
    return &Exporter{
        duration: prometheus.NewGauge(prometheus.GaugeOpts{
            Namespace: namespace,
            Name:      "exporter_last_scrape_duration_seconds",
            Help:      "The last scrape duration.",
        }),
        error: prometheus.NewGauge(prometheus.GaugeOpts{
            Namespace: namespace,
            Name:      "exporter_last_scrape_error",
            Help:      "The last scrape error status.",
        }),
        totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
            Namespace: namespace,
            Name:      "exporter_scrapes_total",
            Help:      "Current total port probe scrapes.",
        }),
        metrics: map[string]prometheus.Gauge{},
    }
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
    for _, m := range e.metrics {
        m.Describe(ch)
    }

    ch <- e.duration.Desc()
    ch <- e.totalScrapes.Desc()
    ch <- e.error.Desc()
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
    scrapes := make(chan []string)

    go e.scrape(scrapes)

    e.mutex.Lock()
    defer e.mutex.Unlock()
    e.setMetrics(scrapes)
    ch <- e.duration
    ch <- e.totalScrapes
    ch <- e.error
    e.collectMetrics(ch)
}

func (e *Exporter) scrape(scrapes chan<- []string) {
    defer close(scrapes)

    now := time.Now().UnixNano()

    e.error.Set(0)

    entries := strings.Split(*probe, ",")
    for _, addr := range entries {
        host, port, err := net.SplitHostPort(strings.Replace(addr, " ", "", -1))
        if err == nil {
            var res []string = make([]string, 2)
            if len(host) == 0 {
                host = "0.0.0.0"
            }
            res[0] = strings.Replace(host, ".", "_", -1) + "_" + port

            check := time.Now().UnixNano()

            // todo open port here
            conn, err := net.Dial("tcp", host+":"+port)
            if err == nil {
                conn.Close()

                if strings.EqualFold(*status, "0") {
                    res[1] = strconv.FormatFloat(float64(time.Now().UnixNano() - check / 1000000000), 'f', -1, 64)
                } else {
                    res[1] = *status
                }

                scrapes <- res
            } else {
                e.error.Inc()
            }
        } else {
            e.error.Inc()
        }
    }

    e.duration.Set(float64(time.Now().UnixNano() - now) / 1000000000)
}

func (e *Exporter) setMetrics(scrapes <-chan []string) {
    for row := range scrapes {
        name := strings.ToLower(row[0])
        value, err := strconv.ParseInt(row[1], 10, 64)
        if err != nil {
            // convert/serve text values here ?
            continue
        }

        if _, ok := e.metrics[name]; !ok {
            e.metrics[name] = prometheus.NewGauge(prometheus.GaugeOpts{
                Namespace: namespace,
                Name:      name,
                })
        }

        e.metrics[name].Set(float64(value))
    }
}

func (e *Exporter) collectMetrics(metrics chan<- prometheus.Metric) {
    for _, m := range e.metrics {
        m.Collect(metrics)
    }
}

func main() {
    flag.Parse()

    exporter := NewPortProbeExporter()
    prometheus.MustRegister(exporter)

    http.Handle(*metricPath, prometheus.Handler())
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte(`<html>
<head><title>port probe exporter</title></head>
<body>
<h1>port probe exporter</h1>
<p><a href='` + *metricPath + `'>Metrics</a></p>
</body>
</html>
`))
    })

    log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
