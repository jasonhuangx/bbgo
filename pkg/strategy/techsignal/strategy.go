package techsignal

import (
	"context"
	"errors"
	"fmt"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/leekchan/accounting"
	"github.com/sirupsen/logrus"
	"strings"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/types"
)

const ID = "techsignal"

var log = logrus.WithField("strategy", ID)

func init() {
	// Register the pointer of the strategy struct,
	// so that bbgo knows what struct to be used to unmarshal the configs (YAML or JSON)
	// Note: built-in strategies need to imported manually in the bbgo cmd package.
	bbgo.RegisterStrategy(ID, &Strategy{})
}

type Strategy struct {
	*bbgo.Notifiability

	// These fields will be filled from the config file (it translates YAML to JSON)
	Symbol string       `json:"symbol"`
	Market types.Market `json:"-"`

	SupportDetection []struct {
		Interval types.Interval `json:"interval"`

		// MovingAverageType is the moving average indicator type that we want to use,
		// it could be SMA or EWMA
		MovingAverageType string `json:"movingAverageType"`

		// MovingAverageInterval is the interval of k-lines for the moving average indicator to calculate,
		// it could be "1m", "5m", "1h" and so on.  note that, the moving averages are calculated from
		// the k-line data we subscribed
		MovingAverageInterval types.Interval `json:"movingAverageInterval"`

		// MovingAverageWindow is the number of the window size of the moving average indicator.
		// The number of k-lines in the window. generally used window sizes are 7, 25 and 99 in the TradingView.
		MovingAverageWindow int `json:"movingAverageWindow"`

		MinVolume fixedpoint.Value `json:"minVolume"`

		MinQuoteVolume fixedpoint.Value `json:"minQuoteVolume"`
	} `json:"supportDetection"`
}

func (s *Strategy) ID() string {
	return ID
}

func (s *Strategy) Subscribe(session *bbgo.ExchangeSession) {
	// session.Subscribe(types.BookChannel, s.Symbol, types.SubscribeOptions{})
	for _, detection := range s.SupportDetection {
		session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{
			Interval: string(detection.Interval),
		})
	}
}

func (s *Strategy) Validate() error {
	if len(s.Symbol) == 0 {
		return errors.New("symbol is required")
	}

	return nil
}

func (s *Strategy) Run(ctx context.Context, orderExecutor bbgo.OrderExecutor, session *bbgo.ExchangeSession) error {
	standardIndicatorSet, ok := session.StandardIndicatorSet(s.Symbol)
	if !ok {
		return fmt.Errorf("standardIndicatorSet is nil, symbol %s", s.Symbol)
	}

	session.MarketDataStream.OnKLineClosed(func(kline types.KLine) {
		// skip k-lines from other symbols
		if kline.Symbol != s.Symbol {
			return
		}

		for _, detection := range s.SupportDetection {
			if kline.Interval != detection.Interval {
				continue
			}

			closePriceF := kline.GetClose()
			closePrice := fixedpoint.NewFromFloat(closePriceF)

			var ma types.Float64Indicator

			switch strings.ToLower(detection.MovingAverageType) {
			case "sma":
				ma = standardIndicatorSet.SMA(types.IntervalWindow{
					Interval: detection.MovingAverageInterval,
					Window:   detection.MovingAverageWindow,
				})
			case "ema", "ewma":
				ma = standardIndicatorSet.EWMA(types.IntervalWindow{
					Interval: detection.MovingAverageInterval,
					Window:   detection.MovingAverageWindow,
				})
			default:
				ma = standardIndicatorSet.EWMA(types.IntervalWindow{
					Interval: detection.MovingAverageInterval,
					Window:   detection.MovingAverageWindow,
				})
			}

			var lastMA = ma.Last()

			// skip if the closed price is above the moving average
			if closePrice.Float64() > lastMA {
				// return
			}

			prettyBaseVolume := accounting.DefaultAccounting(s.Market.BaseCurrency, s.Market.VolumePrecision)
			prettyBaseVolume.Format = "%v %s"

			prettyQuoteVolume := accounting.DefaultAccounting(s.Market.QuoteCurrency, 0)
			prettyQuoteVolume.Format = "%v %s"

			if detection.MinVolume > 0 && kline.Volume > detection.MinVolume.Float64() {
				s.Notifiability.Notify("Detected %s support base volume %s > min base volume %s, quote volume %s",
					s.Symbol,
					prettyBaseVolume.FormatMoney(kline.Volume),
					prettyBaseVolume.FormatMoney(detection.MinVolume.Float64()),
					prettyQuoteVolume.FormatMoney(kline.QuoteVolume),
				)
			} else if detection.MinQuoteVolume > 0 && kline.QuoteVolume > detection.MinQuoteVolume.Float64() {
				s.Notifiability.Notify("Detected %s support quote volume %s > min quote volume %s, base volume %s",
					s.Symbol,
					prettyQuoteVolume.FormatMoney(kline.QuoteVolume),
					prettyQuoteVolume.FormatMoney(detection.MinQuoteVolume.Float64()),
					prettyBaseVolume.FormatMoney(kline.Volume),
				)
			}
		}
	})
	return nil
}
