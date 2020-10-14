package monitor

import (
	"errors"
	"math"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const period = 30

type Metric struct {
	keygenCounter    *prometheus.CounterVec
	keysignCounter   *prometheus.CounterVec
	joinPartyCounter *prometheus.CounterVec
	keySignTime      prometheus.Gauge
	keyGenTime       prometheus.Gauge
	joinPartyTime    *prometheus.GaugeVec
	tssThroughput    *prometheus.GaugeVec
	successRate      *prometheus.GaugeVec

	logger                zerolog.Logger
	ticker                *time.Ticker
	previousSignCounter   float64
	previousKeyGenCounter float64
	SampleStopChan        chan struct{}
}

func GetCounterValue(metric *prometheus.CounterVec, flag string) (float64, error) {
	m := &dto.Metric{}
	if err := metric.WithLabelValues(flag).Write(m); err != nil {
		return 0, err
	}
	return m.Counter.GetValue(), nil
}

func calSuccessRate(a, b float64) (float64, error) {
	if a+b == 0 {
		return 0, errors.New("0 denominator")
	}
	return a / (a + b), nil
}

func successRate(item *prometheus.CounterVec) (float64, error) {
	a, err := GetCounterValue(item, "success")
	if err != nil {
		return 0, err
	}

	b, err := GetCounterValue(item, "failure")
	if err != nil {
		return 0, err
	}
	c, err := calSuccessRate(a, b)
	if err != nil {
		return 0, err
	}
	return c, nil
}

func (m *Metric) UpdateKeyGen(keygenTime time.Duration, success bool) {
	if success {
		m.keyGenTime.Set(float64(keygenTime))
		m.keygenCounter.WithLabelValues("success").Inc()
	} else {
		m.keygenCounter.WithLabelValues("failure").Inc()
	}
	c, err := successRate(m.keygenCounter)
	if err != nil {
		m.logger.Error().Err(err).Msgf("fail to get the keygen success rate")
	}
	m.successRate.WithLabelValues("keygen").Set(c)
}

func (m *Metric) UpdateKeySign(keysignTime time.Duration, success bool) {
	if success {
		m.keySignTime.Set(float64(keysignTime))
		m.keysignCounter.WithLabelValues("success").Inc()
	} else {
		m.keysignCounter.WithLabelValues("failure").Inc()
	}
	c, err := successRate(m.keysignCounter)
	if err != nil {
		m.logger.Error().Err(err).Msgf("fail to get the keysign success rate")
	}
	m.successRate.WithLabelValues("keysign").Set(c)
}

func (m Metric) KeygenJoinParty(joinpartyTime time.Duration, success bool) {
	if success {
		m.joinPartyTime.WithLabelValues("keygen").Set(float64(joinpartyTime))
		m.joinPartyCounter.WithLabelValues("keygen", "success").Inc()
	} else {
		m.joinPartyCounter.WithLabelValues("keygen", "failure").Inc()
	}
}

func (m *Metric) KeysignJoinParty(joinpartyTime time.Duration, success bool) {
	if success {
		m.joinPartyTime.WithLabelValues("keysign").Set(float64(joinpartyTime))
		m.joinPartyCounter.WithLabelValues("keysign", "success").Inc()
	} else {
		m.joinPartyCounter.WithLabelValues("keysign", "failure").Inc()
	}
}

func (m *Metric) updateThroughput() {
	currentKeygen, err1 := GetCounterValue(m.keygenCounter, "success")
	currentKeysign, err2 := GetCounterValue(m.keysignCounter, "success")
	if err1 != nil || err2 != nil {
		m.logger.Error().Msgf("fail to get the current counter value of keygen/keysign")
		return
	}

	deltaKeygen := math.Round((currentKeygen - m.previousKeyGenCounter) / period)
	deltaKeysign := math.Round((currentKeysign - m.previousSignCounter) / period)
	m.previousKeyGenCounter = currentKeygen
	m.previousSignCounter = currentKeysign
	m.tssThroughput.WithLabelValues("keygen").Set(deltaKeygen)
	m.tssThroughput.WithLabelValues("keysign").Set(deltaKeysign)
}

// we make the sample rate as 1/30 to calcualte the keygen/keysign throughtput
func (m *Metric) ThroughputSampleStart() {
	m.logger.Info().Msgf("we start sampling")
	m.ticker = time.NewTicker(time.Second * period)
	go func() {
		for {
			select {
			case <-m.SampleStopChan:
				return
			case <-m.ticker.C:
				m.updateThroughput()
			}
		}
	}()
}

func (m *Metric) ThroughputSamplingStop() {
	m.logger.Info().Msgf("we stop sampling")
	m.ticker.Stop()
	close(m.SampleStopChan)
}

func NewMetric() *Metric {
	metrics := Metric{

		keygenCounter: prometheus.NewCounterVec(

			prometheus.CounterOpts{
				Namespace: "Tss",
				Subsystem: "Tss_monitor",
				Name:      "Tss_keygen",
				Help:      "Tss keygen success and failure counter",
			},
			[]string{"status"},
		),

		keysignCounter: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "Tss",
				Subsystem: "Tss_monitor",
				Name:      "Tss_keysign",
				Help:      "Tss keysign success and failure counter",
			},
			[]string{"status"},
		),

		joinPartyCounter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "Tss",
			Subsystem: "Tss_monitor",
			Name:      "Tss_join_party",
			Help:      "Tss keygen join party success and failure counter",
		}, []string{
			"type", "result",
		}),

		keyGenTime: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "Tss",
				Subsystem: "Tss_monitor",
				Name:      "Tss_keygen_time",
				Help:      "the time spend for the latest keygen",
			},
		),

		keySignTime: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "Tss",
				Subsystem: "Tss_monitor",
				Name:      "Tss_keysign_time",
				Help:      "the time spend for the latest keysign",
			},
		),

		joinPartyTime: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "Tss",
				Subsystem: "Tss_monitor",
				Name:      "Tss_joinparty_time",
				Help:      "the time spend for the latest keysign/keygen join party",
			}, []string{"type"}),

		successRate: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "Tss",
				Subsystem: "Tss_monitor",
				Name:      "Tss_success_rate",
				Help:      "the success rate of keygen or keysign",
			}, []string{"type"}),

		tssThroughput: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "Tss",
				Subsystem: "Tss_monitor",
				Name:      "Tss_throughput",
				Help:      "the throughput of keysign/keygen every 30 seconds",
			}, []string{"type"}),

		logger:         log.With().Str("module", "tssMonitor").Logger(),
		SampleStopChan: make(chan struct{}),
	}

	metrics.tssThroughput.WithLabelValues("keygen").Set(0)
	metrics.tssThroughput.WithLabelValues("keysign").Set(0)

	prometheus.MustRegister(metrics.keygenCounter)
	prometheus.MustRegister(metrics.keysignCounter)
	prometheus.MustRegister(metrics.joinPartyCounter)
	prometheus.MustRegister(metrics.keyGenTime)
	prometheus.MustRegister(metrics.keySignTime)
	prometheus.MustRegister(metrics.joinPartyTime)
	prometheus.MustRegister(metrics.successRate)
	prometheus.MustRegister(metrics.tssThroughput)

	return &metrics
}
