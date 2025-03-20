package monitor

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Metric struct {
	keygenCounter    *prometheus.CounterVec
	keysignCounter   *prometheus.CounterVec
	joinPartyCounter *prometheus.CounterVec
	keySignTime      prometheus.Gauge
	keyGenTime       prometheus.Gauge
	joinPartyTime    *prometheus.GaugeVec
	logger           zerolog.Logger
}

func (m *Metric) UpdateKeyGen(keygenTime time.Duration, success bool) {
	if success {
		m.keyGenTime.Set(float64(keygenTime))
		m.keygenCounter.WithLabelValues("success").Inc()
	} else {
		m.keygenCounter.WithLabelValues("failure").Inc()
	}
}

func (m *Metric) UpdateKeySign(keysignTime time.Duration, success bool) {
	if success {
		m.keySignTime.Set(float64(keysignTime))
		m.keysignCounter.WithLabelValues("success").Inc()
	} else {
		m.keysignCounter.WithLabelValues("failure").Inc()
	}
}

func (m Metric) KeygenJoinParty(joinPartyTime time.Duration, success bool) {
	if success {
		m.joinPartyTime.WithLabelValues("keygen").Set(float64(joinPartyTime))
		m.joinPartyCounter.WithLabelValues("keygen", "success").Inc()
		return
	}

	m.joinPartyCounter.WithLabelValues("keygen", "failure").Inc()
}

func (m *Metric) KeysignJoinParty(joinPartyTime time.Duration, success bool) {
	if success {
		m.joinPartyTime.WithLabelValues("keysign").Set(float64(joinPartyTime))
		m.joinPartyCounter.WithLabelValues("keysign", "success").Inc()
		return
	}

	m.joinPartyCounter.WithLabelValues("keysign", "failure").Inc()
}

func (m *Metric) Enable() {
	prometheus.MustRegister(m.keygenCounter)
	prometheus.MustRegister(m.keysignCounter)
	prometheus.MustRegister(m.joinPartyCounter)
	prometheus.MustRegister(m.keyGenTime)
	prometheus.MustRegister(m.keySignTime)
	prometheus.MustRegister(m.joinPartyTime)
}

func NewMetric() *Metric {
	metrics := Metric{

		keygenCounter: prometheus.NewCounterVec(

			prometheus.CounterOpts{
				Namespace: "Tss",
				Subsystem: "Tss",
				Name:      "keygen",
				Help:      "Tss keygen success and failure counter",
			},
			[]string{"status"},
		),

		keysignCounter: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "Tss",
				Subsystem: "Tss",
				Name:      "keysign",
				Help:      "Tss keysign success and failure counter",
			},
			[]string{"status"},
		),

		joinPartyCounter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "Tss",
			Subsystem: "Tss",
			Name:      "join_party",
			Help:      "Tss keygen join party success and failure counter",
		}, []string{
			"type", "result",
		}),

		keyGenTime: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "Tss",
				Subsystem: "Tss",
				Name:      "keygen_time",
				Help:      "the time spend for the latest keygen",
			},
		),

		keySignTime: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "Tss",
				Subsystem: "Tss",
				Name:      "keysign_time",
				Help:      "the time spend for the latest keysign",
			},
		),

		joinPartyTime: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "Tss",
				Subsystem: "Tss",
				Name:      "joinparty_time",
				Help:      "the time spend for the latest keysign/keygen join party",
			}, []string{"type"}),

		logger: log.With().Str("module", "tssMonitor").Logger(),
	}
	return &metrics
}
