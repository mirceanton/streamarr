package handlers

import (
	"net/http"

	"github.com/mirceanton/streamarr/internal/db"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type streamarrCollector struct {
	mediaTotal          *prometheus.Desc
	mediaNeedsAttention *prometheus.Desc
	jobsTotal           *prometheus.Desc
	healthPercent       *prometheus.Desc
}

func newStreamarrCollector() *streamarrCollector {
	return &streamarrCollector{
		mediaTotal: prometheus.NewDesc(
			"streamarr_media_total",
			"Total number of media items",
			[]string{"type"}, nil,
		),
		mediaNeedsAttention: prometheus.NewDesc(
			"streamarr_media_needs_attention_total",
			"Number of media items that need attention",
			[]string{"type"}, nil,
		),
		jobsTotal: prometheus.NewDesc(
			"streamarr_jobs_total",
			"Number of jobs by status",
			[]string{"status"}, nil,
		),
		healthPercent: prometheus.NewDesc(
			"streamarr_health_percent",
			"Percentage of media files matching language and subtitle format preferences",
			nil, nil,
		),
	}
}

func (c *streamarrCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.mediaTotal
	ch <- c.mediaNeedsAttention
	ch <- c.jobsTotal
	ch <- c.healthPercent
}

func (c *streamarrCollector) Collect(ch chan<- prometheus.Metric) {
	stats, err := db.GetDashboardStats()
	if err != nil {
		return
	}

	ch <- prometheus.MustNewConstMetric(c.mediaTotal, prometheus.GaugeValue, float64(stats.TotalMovies), "movie")
	ch <- prometheus.MustNewConstMetric(c.mediaTotal, prometheus.GaugeValue, float64(stats.TotalSeries), "series")
	ch <- prometheus.MustNewConstMetric(c.mediaTotal, prometheus.GaugeValue, float64(stats.TotalEpisodes), "episode")

	ch <- prometheus.MustNewConstMetric(c.mediaNeedsAttention, prometheus.GaugeValue, float64(stats.MoviesNeedAttention), "movie")
	ch <- prometheus.MustNewConstMetric(c.mediaNeedsAttention, prometheus.GaugeValue, float64(stats.SeriesNeedAttention), "series")
	ch <- prometheus.MustNewConstMetric(c.mediaNeedsAttention, prometheus.GaugeValue, float64(stats.EpisodesNeedAttention), "episode")

	ch <- prometheus.MustNewConstMetric(c.jobsTotal, prometheus.GaugeValue, float64(stats.TotalJobs), "total")
	ch <- prometheus.MustNewConstMetric(c.jobsTotal, prometheus.GaugeValue, float64(stats.RunningJobs), "running")
	ch <- prometheus.MustNewConstMetric(c.jobsTotal, prometheus.GaugeValue, float64(stats.PendingJobs), "pending")

	ch <- prometheus.MustNewConstMetric(c.healthPercent, prometheus.GaugeValue, float64(stats.HealthPct))
}

func MetricsHandler() http.Handler {
	registry := prometheus.NewRegistry()
	registry.MustRegister(newStreamarrCollector())
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}
