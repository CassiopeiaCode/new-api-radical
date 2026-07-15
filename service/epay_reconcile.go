package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

type EpayOrderQueryResult struct {
	Code       int    `json:"code"`
	Message    string `json:"msg"`
	Status     int    `json:"status"`
	Pid        string `json:"pid"`
	Type       string `json:"type"`
	Money      string `json:"money"`
	TradeNo    string `json:"trade_no"`
	OutTradeNo string `json:"out_trade_no"`
}

type EpayReconcileOptions struct {
	Limit         int   `json:"limit"`
	MinAgeSeconds int64 `json:"min_age_seconds"`
	MaxAgeSeconds int64 `json:"max_age_seconds"`
	DryRun        bool  `json:"dry_run"`
}

type EpayReconcileItem struct {
	OrderType string `json:"order_type"`
	TradeNo   string `json:"trade_no"`
	Action    string `json:"action"`
	Error     string `json:"error,omitempty"`
}

type EpayReconcileReport struct {
	Scanned   int                 `json:"scanned"`
	Completed int                 `json:"completed"`
	Skipped   int                 `json:"skipped"`
	Failed    int                 `json:"failed"`
	DryRun    bool                `json:"dry_run"`
	Items     []EpayReconcileItem `json:"items"`
}

// ReconcilePendingEpayOrders checks only local pending EPay rows. It is safe
// to call concurrently with webhooks because terminal writes are idempotent.
func ReconcilePendingEpayOrders(opts EpayReconcileOptions) EpayReconcileReport {
	report := EpayReconcileReport{DryRun: opts.DryRun, Items: make([]EpayReconcileItem, 0)}
	topUps, err := model.GetPendingEpayTopUps(opts.Limit, opts.MinAgeSeconds, opts.MaxAgeSeconds)
	if err != nil {
		report.Failed++
		report.Items = append(report.Items, EpayReconcileItem{OrderType: "topup", Action: "local_query_failed", Error: err.Error()})
	} else {
		for _, topUp := range topUps {
			report.add(reconcileTopUp(topUp, opts.DryRun))
		}
	}
	orders, err := model.GetPendingEpaySubscriptionOrders(opts.Limit, opts.MinAgeSeconds, opts.MaxAgeSeconds)
	if err != nil {
		report.Failed++
		report.Items = append(report.Items, EpayReconcileItem{OrderType: "subscription", Action: "local_query_failed", Error: err.Error()})
	} else {
		for _, order := range orders {
			report.add(reconcileSubscription(order, opts.DryRun))
		}
	}
	return report
}

func (r *EpayReconcileReport) add(item EpayReconcileItem) {
	r.Scanned++
	r.Items = append(r.Items, item)
	switch item.Action {
	case "completed":
		r.Completed++
	case "would_complete", "provider_pending", "provider_not_found", "validation_failed":
		r.Skipped++
	default:
		r.Failed++
	}
}

func reconcileTopUp(topUp *model.TopUp, dryRun bool) EpayReconcileItem {
	item := EpayReconcileItem{OrderType: "topup", TradeNo: topUp.TradeNo}
	result, err := QueryEpayOrder(topUp.TradeNo)
	if err != nil {
		item.Action = "provider_query_failed"
		item.Error = err.Error()
		return item
	}
	if err := validateEpayResult(result, topUp.TradeNo, topUp.Money); err != nil {
		item.Action = "validation_failed"
		item.Error = err.Error()
		return item
	}
	if result.Status != 1 {
		item.Action = "provider_pending"
		return item
	}
	if dryRun {
		item.Action = "would_complete"
		return item
	}
	if err := model.CompleteEpayTopUp(topUp.TradeNo, result.Type, ""); err != nil {
		item.Action = "complete_failed"
		item.Error = err.Error()
		return item
	}
	item.Action = "completed"
	return item
}

func reconcileSubscription(order *model.SubscriptionOrder, dryRun bool) EpayReconcileItem {
	item := EpayReconcileItem{OrderType: "subscription", TradeNo: order.TradeNo}
	result, err := QueryEpayOrder(order.TradeNo)
	if err != nil {
		item.Action = "provider_query_failed"
		item.Error = err.Error()
		return item
	}
	if err := validateEpayResult(result, order.TradeNo, order.Money); err != nil {
		item.Action = "validation_failed"
		item.Error = err.Error()
		return item
	}
	if result.Status != 1 {
		item.Action = "provider_pending"
		return item
	}
	if dryRun {
		item.Action = "would_complete"
		return item
	}
	payload, _ := json.Marshal(result)
	if err := model.CompleteSubscriptionOrder(order.TradeNo, string(payload), model.PaymentProviderEpay, result.Type); err != nil {
		item.Action = "complete_failed"
		item.Error = err.Error()
		return item
	}
	item.Action = "completed"
	return item
}

func validateEpayResult(result *EpayOrderQueryResult, tradeNo string, money float64) error {
	if result.Code != 1 {
		return errors.New("provider did not find a successful query result")
	}
	if result.OutTradeNo != tradeNo {
		return errors.New("provider out_trade_no mismatch")
	}
	if result.Pid != operation_setting.EpayId {
		return errors.New("provider pid mismatch")
	}
	amount, err := strconv.ParseFloat(strings.TrimSpace(result.Money), 64)
	if err != nil || math.Abs(amount-money) >= 0.01 {
		return errors.New("provider money mismatch")
	}
	return nil
}

func QueryEpayOrder(outTradeNo string) (*EpayOrderQueryResult, error) {
	if outTradeNo == "" || operation_setting.PayAddress == "" || operation_setting.EpayId == "" || operation_setting.EpayKey == "" {
		return nil, errors.New("epay settings or order number are incomplete")
	}
	u, err := url.Parse(operation_setting.PayAddress)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("invalid epay address")
	}
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/pay") + "/api.php"
	q := u.Query()
	q.Set("act", "order")
	q.Set("pid", operation_setting.EpayId)
	q.Set("key", operation_setting.EpayKey)
	q.Set("out_trade_no", outTradeNo)
	u.RawQuery = q.Encode()
	seconds := common.GetEnvOrDefault("EPAY_ORDER_RECONCILE_HTTP_TIMEOUT_SECONDS", 15)
	client := http.Client{Timeout: time.Duration(seconds) * time.Second}
	resp, err := client.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("epay query http status %d", resp.StatusCode)
	}
	var result EpayOrderQueryResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
