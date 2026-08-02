package service

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// BillingSession 封装单次请求的钱包预扣费、结算和退款生命周期。
type BillingSession struct {
	relayInfo        *relaycommon.RelayInfo
	preConsumedQuota int
	walletConsumed   int
	tokenConsumed    int
	trusted          bool
	settled          bool
	settledQuota     int
	refunded         bool
	mu               sync.Mutex
}

func (s *BillingSession) Settle(actualQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled {
		return nil
	}
	delta := actualQuota - s.preConsumedQuota
	if delta != 0 {
		tokenID := s.relayInfo.TokenId
		if s.relayInfo.IsPlayground {
			tokenID = 0
		}
		if err := model.AdjustBillingQuotaAtomic(model.BillingQuotaAdjustment{
			UserID:   s.relayInfo.UserId,
			TokenID:  tokenID,
			TokenKey: s.relayInfo.TokenKey,
			Delta:    delta,
		}); err != nil {
			return err
		}
	}
	s.settledQuota = actualQuota
	s.settled = true
	return nil
}

func (s *BillingSession) Compensate(c *gin.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refunded {
		return nil
	}
	if !s.settled {
		return fmt.Errorf("billing session is not settled")
	}
	tokenID := s.relayInfo.TokenId
	tokenAmount := s.settledQuota
	if s.relayInfo.IsPlayground {
		tokenID = 0
		tokenAmount = 0
	}
	if err := model.RefundBillingQuotaAtomic(model.BillingQuotaRefund{
		UserID:        s.relayInfo.UserId,
		TokenID:       tokenID,
		TokenKey:      s.relayInfo.TokenKey,
		FundingAmount: s.settledQuota,
		TokenAmount:   tokenAmount,
	}); err != nil {
		return err
	}
	s.refunded = true
	logger.LogInfo(c, fmt.Sprintf("用户 %d 的任务记录落库失败，已冲正结算额度 %s", s.relayInfo.UserId, logger.FormatQuota(s.settledQuota)))
	return nil
}

func (s *BillingSession) Refund(c *gin.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled || s.refunded || !s.needsRefundLocked() {
		return
	}
	logger.LogInfo(c, fmt.Sprintf("用户 %d 请求失败, 返还预扣费（token_quota=%s）",
		s.relayInfo.UserId, logger.FormatQuota(s.tokenConsumed)))
	tokenID := s.relayInfo.TokenId
	tokenAmount := s.tokenConsumed
	if s.relayInfo.IsPlayground {
		tokenID = 0
		tokenAmount = 0
	}
	if err := model.RefundBillingQuotaAtomic(model.BillingQuotaRefund{
		UserID:        s.relayInfo.UserId,
		TokenID:       tokenID,
		TokenKey:      s.relayInfo.TokenKey,
		FundingAmount: s.walletConsumed,
		TokenAmount:   tokenAmount,
	}); err != nil {
		common.SysLog("error refunding billing quota atomically: " + err.Error())
		return
	}
	s.refunded = true
}

func (s *BillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsRefundLocked()
}

func (s *BillingSession) needsRefundLocked() bool {
	return !s.settled && !s.refunded && (s.walletConsumed > 0 || s.tokenConsumed > 0)
}

func (s *BillingSession) GetPreConsumedQuota() int {
	return s.preConsumedQuota
}

func (s *BillingSession) Reserve(targetQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled || s.refunded || s.trusted || targetQuota <= s.preConsumedQuota {
		return nil
	}
	delta := targetQuota - s.preConsumedQuota
	if err := model.DecreaseUserQuota(s.relayInfo.UserId, delta, false); err != nil {
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	if err := s.reserveToken(delta); err != nil {
		if rollbackErr := model.IncreaseUserQuota(s.relayInfo.UserId, delta, false); rollbackErr != nil {
			common.SysLog("error rolling back wallet funding reserve: " + rollbackErr.Error())
		}
		return err
	}
	s.walletConsumed += delta
	s.preConsumedQuota += delta
	s.tokenConsumed += delta
	s.syncRelayInfo()
	return nil
}

func (s *BillingSession) preConsume(c *gin.Context, quota int) *types.NewAPIError {
	effectiveQuota := quota
	if s.shouldTrust(c) {
		s.trusted = true
		effectiveQuota = 0
		logger.LogInfo(c, fmt.Sprintf("用户 %d 额度充足, 信任且不需要预扣费", s.relayInfo.UserId))
	} else if effectiveQuota > 0 {
		logger.LogInfo(c, fmt.Sprintf("用户 %d 需要预扣费 %s", s.relayInfo.UserId, logger.FormatQuota(effectiveQuota)))
	}
	if effectiveQuota > 0 {
		if err := PreConsumeTokenQuota(s.relayInfo, effectiveQuota); err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		s.tokenConsumed = effectiveQuota
		if err := model.DecreaseUserQuota(s.relayInfo.UserId, effectiveQuota, false); err != nil {
			if !s.relayInfo.IsPlayground {
				if rollbackErr := model.IncreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, s.tokenConsumed); rollbackErr != nil {
					common.SysLog("error rolling back token quota: " + rollbackErr.Error())
				}
			}
			s.tokenConsumed = 0
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		s.walletConsumed = effectiveQuota
	}
	s.preConsumedQuota = effectiveQuota
	s.syncRelayInfo()
	return nil
}

func (s *BillingSession) reserveToken(delta int) error {
	if delta <= 0 || s.relayInfo.IsPlayground {
		return nil
	}
	if err := PreConsumeTokenQuota(s.relayInfo, delta); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	return nil
}

func (s *BillingSession) shouldTrust(c *gin.Context) bool {
	if s.relayInfo.ForcePreConsume {
		return false
	}
	trustQuota := common.GetTrustQuota()
	if trustQuota <= 0 {
		return false
	}
	tokenTrusted := s.relayInfo.TokenUnlimited
	if !tokenTrusted {
		tokenTrusted = c.GetInt("token_quota") > trustQuota
	}
	return tokenTrusted && s.relayInfo.UserQuota > trustQuota
}

func (s *BillingSession) syncRelayInfo() {
	s.relayInfo.FinalPreConsumedQuota = s.preConsumedQuota
}

func NewBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError) {
	if relayInfo == nil {
		return nil, types.NewError(fmt.Errorf("relayInfo is nil"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if userQuota <= 0 || userQuota < preConsumedQuota {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("用户额度不足, 剩余额度: %s, 需要预扣费额度: %s", logger.FormatQuota(userQuota), logger.FormatQuota(preConsumedQuota)),
			types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
			types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	relayInfo.UserQuota = userQuota
	session := &BillingSession{relayInfo: relayInfo}
	if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
		return nil, apiErr
	}
	return session, nil
}
