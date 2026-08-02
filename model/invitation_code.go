package model

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"gorm.io/gorm"
)

const (
	InvitationCodeStatusEnabled  = 1
	InvitationCodeStatusDisabled = 2
)

type InvitationCode struct {
	Id          int            `json:"id"`
	Code        string         `json:"code" gorm:"type:varchar(32);uniqueIndex"`
	Status      int            `json:"status" gorm:"type:int;default:1"`
	Name        string         `json:"name" gorm:"type:varchar(64);index"`
	MaxUses     int            `json:"max_uses" gorm:"type:int;default:1"`
	UsedCount   int            `json:"used_count" gorm:"type:int;default:0"`
	CreatedBy   int            `json:"created_by" gorm:"type:int;index"`
	OwnerUserId int            `json:"owner_user_id" gorm:"type:int;column:owner_user_id;index"`
	CreatedTime int64          `json:"created_time" gorm:"bigint"`
	ExpiredTime int64          `json:"expired_time" gorm:"bigint"`
	IsAdmin     bool           `json:"is_admin" gorm:"default:false"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
	Count       int            `json:"count" gorm:"-:all"`
}

type InvitationCodeUsage struct {
	Id               int    `json:"id"`
	InvitationCodeId int    `json:"invitation_code_id" gorm:"index"`
	UserId           int    `json:"user_id" gorm:"index"`
	Username         string `json:"username" gorm:"type:varchar(64);index"`
	UsedTime         int64  `json:"used_time" gorm:"bigint"`
}

type InvitationCodeQuotaInfo struct {
	Limit                int   `json:"limit"`
	Used                 int64 `json:"used"`
	Remaining            int   `json:"remaining"`
	AccountAgeDays       int   `json:"account_age_days"`
	MinAccountAgeDays    int   `json:"min_account_age_days"`
	DefaultCodeMaxUses   int   `json:"default_code_max_uses"`
	DefaultCodeValidDays int   `json:"default_code_valid_days"`
	// 月度滚动窗口配额（近 30 天）。MonthlyLimit=0 表示不限制。
	MonthlyLimit     int   `json:"monthly_limit"`
	MonthlyUsed      int64 `json:"monthly_used"`
	MonthlyRemaining int   `json:"monthly_remaining"`
	// MonthlyNextResetAt 是窗口里"最早一条邀请码"出窗的时间戳（秒）；
	// 还没生成过 / 不限制时为 0，前端可据此显示"X 天后可再生成"。
	MonthlyNextResetAt int64 `json:"monthly_next_reset_at"`
	// 最低累计充值门槛与该用户当前累计成功充值额（人民币）；MinTopupMoney=0 表示策略不启用。
	MinTopupMoney  float64 `json:"min_topup_money"`
	UserTopupMoney float64 `json:"user_topup_money"`
}

func (invitationCode *InvitationCode) IsExpired() bool {
	return invitationCode.ExpiredTime != 0 && invitationCode.ExpiredTime < common.GetTimestamp()
}

func (invitationCode *InvitationCode) IsExhausted() bool {
	return invitationCode.MaxUses > 0 && invitationCode.UsedCount >= invitationCode.MaxUses
}

func validateInvitationCodeRecord(invitationCode *InvitationCode) error {
	if invitationCode.Status != InvitationCodeStatusEnabled {
		return ErrInvitationCodeDisabled
	}
	if invitationCode.IsExpired() {
		return ErrInvitationCodeExpired
	}
	if invitationCode.IsExhausted() {
		return ErrInvitationCodeExhausted
	}
	return nil
}

func ValidateInvitationCode(code string) (*InvitationCode, error) {
	trimmedCode := strings.TrimSpace(code)
	if trimmedCode == "" {
		return nil, ErrInvitationCodeRequired
	}
	invitationCode := &InvitationCode{}
	if err := DB.Where("code = ?", trimmedCode).First(invitationCode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvitationCodeInvalid
		}
		return nil, err
	}
	if err := validateInvitationCodeRecord(invitationCode); err != nil {
		return nil, err
	}
	return invitationCode, nil
}

func GetUsableInvitationCodeWithTx(tx *gorm.DB, code string) (*InvitationCode, error) {
	trimmedCode := strings.TrimSpace(code)
	if trimmedCode == "" {
		return nil, ErrInvitationCodeRequired
	}
	invitationCode := &InvitationCode{}
	query := tx
	if !common.UsingSQLite {
		query = query.Set("gorm:query_option", "FOR UPDATE")
	}
	if err := query.Where("code = ?", trimmedCode).First(invitationCode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvitationCodeInvalid
		}
		return nil, err
	}
	if err := validateInvitationCodeRecord(invitationCode); err != nil {
		return nil, err
	}
	return invitationCode, nil
}

func ConsumeInvitationCodeWithTx(tx *gorm.DB, invitationCode *InvitationCode, userId int, username string) error {
	if invitationCode == nil {
		return nil
	}
	if err := validateInvitationCodeRecord(invitationCode); err != nil {
		return err
	}
	if invitationCode.MaxUses > 0 && invitationCode.UsedCount+1 > invitationCode.MaxUses {
		return ErrInvitationCodeExhausted
	}
	if err := tx.Model(invitationCode).Update("used_count", gorm.Expr("used_count + ?", 1)).Error; err != nil {
		return err
	}
	invitationCode.UsedCount++
	usage := &InvitationCodeUsage{
		InvitationCodeId: invitationCode.Id,
		UserId:           userId,
		Username:         username,
		UsedTime:         common.GetTimestamp(),
	}
	return tx.Create(usage).Error
}

func (invitationCode *InvitationCode) Insert() error {
	return DB.Create(invitationCode).Error
}

func (invitationCode *InvitationCode) Update() error {
	updates := map[string]any{
		"name":          invitationCode.Name,
		"status":        invitationCode.Status,
		"max_uses":      invitationCode.MaxUses,
		"expired_time":  invitationCode.ExpiredTime,
		"owner_user_id": invitationCode.OwnerUserId,
	}
	return DB.Model(invitationCode).Updates(updates).Error
}

func GetInvitationCodeById(id int) (*InvitationCode, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	invitationCode := &InvitationCode{}
	if err := DB.First(invitationCode, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return invitationCode, nil
}

func GetAllInvitationCodes(startIdx int, num int) ([]*InvitationCode, int64, error) {
	var invitationCodes []*InvitationCode
	var total int64
	query := DB.Model(&InvitationCode{})
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Order("id desc").Limit(num).Offset(startIdx).Find(&invitationCodes).Error; err != nil {
		return nil, 0, err
	}
	return invitationCodes, total, nil
}

func SearchInvitationCodes(keyword string, startIdx int, num int) ([]*InvitationCode, int64, error) {
	var invitationCodes []*InvitationCode
	var total int64
	query := DB.Model(&InvitationCode{})
	if id, err := strconv.Atoi(keyword); err == nil {
		query = query.Where("id = ? OR owner_user_id = ? OR created_by = ? OR name LIKE ? OR code LIKE ?", id, id, id, keyword+"%", keyword+"%")
	} else {
		query = query.Where("name LIKE ? OR code LIKE ?", keyword+"%", keyword+"%")
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Order("id desc").Limit(num).Offset(startIdx).Find(&invitationCodes).Error; err != nil {
		return nil, 0, err
	}
	return invitationCodes, total, nil
}

func GetInvitationCodeUsages(invitationCodeId int) ([]*InvitationCodeUsage, error) {
	var usages []*InvitationCodeUsage
	err := DB.Where("invitation_code_id = ?", invitationCodeId).Order("id desc").Find(&usages).Error
	return usages, err
}

func DeleteInvitationCodeById(id int) error {
	if id == 0 {
		return errors.New("id 为空！")
	}
	return DB.Delete(&InvitationCode{}, "id = ?", id).Error
}

func DeleteInvalidInvitationCodes() (int64, error) {
	now := common.GetTimestamp()
	result := DB.Where(
		"status = ? OR (expired_time != 0 AND expired_time < ?) OR (max_uses != 0 AND used_count >= max_uses)",
		InvitationCodeStatusDisabled,
		now,
	).Delete(&InvitationCode{})
	return result.RowsAffected, result.Error
}

func GetOwnedInvitationCodesByUserId(userId int) ([]*InvitationCode, error) {
	var invitationCodes []*InvitationCode
	err := DB.Where("owner_user_id = ?", userId).Order("id desc").Find(&invitationCodes).Error
	return invitationCodes, err
}

func getUserInvitationCodeGenerateLimit(user *User) int {
	policy := setting.GetInvitationCodePolicy()
	if quota, ok := policy.RoleQuotas[strconv.Itoa(user.Role)]; ok {
		return quota
	}
	if quota, ok := policy.GroupQuotas[user.Group]; ok {
		return quota
	}
	return policy.DefaultGenerateQuota
}

func countUserCreatedInvitationCodes(userId int) (int64, error) {
	var count int64
	err := DB.Unscoped().
		Model(&InvitationCode{}).
		Where("created_by = ? AND is_admin = ?", userId, false).
		Count(&count).Error
	return count, err
}

// countUserCreatedInvitationCodesSince 计算用户在 since 之后（含）创建的非 admin 邀请码数量。
// 用于"近 30 天滑动窗口"配额。同样走 Unscoped()，避免用户删一条就空出新名额。
func countUserCreatedInvitationCodesSince(userId int, since int64) (int64, error) {
	var count int64
	err := DB.Unscoped().
		Model(&InvitationCode{}).
		Where("created_by = ? AND is_admin = ? AND created_time >= ?", userId, false, since).
		Count(&count).Error
	return count, err
}

// oldestUserInvitationCodeCreatedTimeSince 返回用户在 since 之后创建的最早一条邀请码的 created_time。
// 找不到返回 0。用于算"下一次月度配额释放时刻"。
func oldestUserInvitationCodeCreatedTimeSince(userId int, since int64) (int64, error) {
	var record InvitationCode
	err := DB.Unscoped().
		Model(&InvitationCode{}).
		Where("created_by = ? AND is_admin = ? AND created_time >= ?", userId, false, since).
		Order("created_time asc").
		Limit(1).
		Find(&record).Error
	if err != nil {
		return 0, err
	}
	return record.CreatedTime, nil
}

func GetUserInvitationCodeQuotaInfo(user *User) (*InvitationCodeQuotaInfo, error) {
	used, err := countUserCreatedInvitationCodes(user.Id)
	if err != nil {
		return nil, err
	}
	policy := setting.GetInvitationCodePolicy()
	accountAgeDays := policy.MinAccountAgeDays
	if user.CreatedTime > 0 {
		accountAgeDays = int((common.GetTimestamp() - user.CreatedTime) / 86400)
		if accountAgeDays < 0 {
			accountAgeDays = 0
		}
	}
	limit := getUserInvitationCodeGenerateLimit(user)
	remaining := -1
	if limit >= 0 {
		remaining = limit - int(used)
		if remaining < 0 {
			remaining = 0
		}
	}

	// 月度滑动窗口配额。policy.MonthlyGenerateQuota 取自全局策略（admin 不分组覆盖月度，
	// 想要分角色月度限额可后续再扩 RoleMonthlyQuotas / GroupMonthlyQuotas）。
	monthlyLimit := policy.MonthlyGenerateQuota
	var monthlyUsed int64
	monthlyRemaining := -1
	var monthlyNextResetAt int64
	if monthlyLimit > 0 {
		now := common.GetTimestamp()
		since := now - setting.MonthlyQuotaWindowSeconds
		monthlyUsed, err = countUserCreatedInvitationCodesSince(user.Id, since)
		if err != nil {
			return nil, err
		}
		monthlyRemaining = monthlyLimit - int(monthlyUsed)
		if monthlyRemaining < 0 {
			monthlyRemaining = 0
		}
		// 窗口里最早一条 +30d 就是下一次配额释放时刻。
		oldest, err := oldestUserInvitationCodeCreatedTimeSince(user.Id, since)
		if err != nil {
			return nil, err
		}
		if oldest > 0 {
			monthlyNextResetAt = oldest + setting.MonthlyQuotaWindowSeconds
		}
	}

	// 最低累计充值门槛。policy.MinTopupMoney<=0 直接跳过查询，避免给所有正常用户多打一次 SUM。
	var userTopupMoney float64
	if policy.MinTopupMoney > 0 {
		userTopupMoney, err = SumUserSuccessTopupMoney(user.Id)
		if err != nil {
			return nil, err
		}
	}

	return &InvitationCodeQuotaInfo{
		Limit:                limit,
		Used:                 used,
		Remaining:            remaining,
		AccountAgeDays:       accountAgeDays,
		MinAccountAgeDays:    policy.MinAccountAgeDays,
		DefaultCodeMaxUses:   policy.DefaultCodeMaxUses,
		DefaultCodeValidDays: policy.DefaultCodeValidDays,
		MonthlyLimit:         monthlyLimit,
		MonthlyUsed:          monthlyUsed,
		MonthlyRemaining:     monthlyRemaining,
		MonthlyNextResetAt:   monthlyNextResetAt,
		MinTopupMoney:        policy.MinTopupMoney,
		UserTopupMoney:       userTopupMoney,
	}, nil
}

func CanUserGenerateInvitationCode(user *User, quotaInfo *InvitationCodeQuotaInfo) error {
	if !common.InvitationCodeUserGenerateEnabled {
		// eflow: 客服 (role>=5) / admin (role>=10) 即使 user-gen 全局开关关闭也允许生成。
		// 这是「客服无限生成」语义的来源——admin 无限是历史行为（走 /api/invitation_code/，
		// 不经过本闸）；本分支让客服走 /api/user/invitation_codes 时同样不受全局闸约束。
		if user != nil && user.Role >= common.RoleCustomerServiceUser {
			return nil
		}
		return ErrInvitationCodeUserGenerateDisabled
	}
	// eflow: 客服/管理员旁路全部用户侧门槛（账龄/累计充值/终身配额/月度配额）。
	// 普通用户继续走完整链路。这条放在 enable 开关之后、各门槛之前。
	if user != nil && user.Role >= common.RoleCustomerServiceUser {
		return nil
	}
	if quotaInfo == nil {
		var err error
		quotaInfo, err = GetUserInvitationCodeQuotaInfo(user)
		if err != nil {
			return err
		}
	}
	if user.CreatedTime > 0 && quotaInfo.AccountAgeDays < quotaInfo.MinAccountAgeDays {
		return ErrInvitationCodeAccountTooYoung
	}
	// 累计充值门槛：把"链式裂变"的成本从"等账龄"换成"真实付费"。0 = 不启用。
	if quotaInfo.MinTopupMoney > 0 && quotaInfo.UserTopupMoney < quotaInfo.MinTopupMoney {
		return ErrInvitationCodeTopupNotEnough
	}
	if quotaInfo.Limit == 0 {
		return ErrInvitationCodeQuotaExceeded
	}
	if quotaInfo.Limit > 0 && quotaInfo.Used >= int64(quotaInfo.Limit) {
		return ErrInvitationCodeQuotaExceeded
	}
	// 月度滑动窗口配额。0 = 不限制，跳过。
	if quotaInfo.MonthlyLimit > 0 && quotaInfo.MonthlyUsed >= int64(quotaInfo.MonthlyLimit) {
		return ErrInvitationCodeMonthlyQuotaExceeded
	}
	return nil
}

// GenerateUserInvitationCodeOptions —— eflow: 客服/管理员可选自定义生成参数。
//   - Count:        生成个数 (1-100)；0 视为 1。普通用户调用方应一律传 0/1。
//   - MaxUses:      每个码的使用次数上限；<0 视为按全局策略 DefaultCodeMaxUses。
//   - ExpiredTime:  Unix 秒；负数视为按全局策略 DefaultCodeValidDays 兜底。0 = 永久。
//   - Name:         邀请码名称；空字符串走默认 "用户邀请码"。
// 普通用户路径仍可传 nil 选项，行为字节级等同于改造前。
type GenerateUserInvitationCodeOptions struct {
	Count       int
	MaxUses     int
	ExpiredTime int64
	Name        string
	HasMaxUses  bool // 区分"未传"与"传 0(=无限次)"
	HasExpired  bool // 区分"未传"与"传 0(=永久)"
}

func GenerateUserInvitationCode(user *User, opts *GenerateUserInvitationCodeOptions) ([]*InvitationCode, error) {
	policy := setting.GetInvitationCodePolicy()
	count := 1
	maxUses := policy.DefaultCodeMaxUses
	var expiredTime int64
	if policy.DefaultCodeValidDays > 0 {
		expiredTime = -1 // 占位标记：稍后用 createdTime + DefaultCodeValidDays 算
	}
	name := "用户邀请码"
	if opts != nil {
		if opts.Count > 0 {
			count = opts.Count
		}
		if count > 100 {
			count = 100
		}
		if opts.HasMaxUses {
			if opts.MaxUses < 0 {
				return nil, ErrInvitationCodeInvalid
			}
			maxUses = opts.MaxUses
		}
		if opts.HasExpired {
			expiredTime = opts.ExpiredTime
		}
		if trimmed := strings.TrimSpace(opts.Name); trimmed != "" {
			name = trimmed
		}
	}

	codes := make([]*InvitationCode, 0, count)
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 配额检查只做一次：普通用户路径下 count 应=1；客服/admin 旁路 quotaInfo。
		quotaInfo, err := GetUserInvitationCodeQuotaInfo(user)
		if err != nil {
			return err
		}
		if err := CanUserGenerateInvitationCode(user, quotaInfo); err != nil {
			return err
		}
		now := common.GetTimestamp()
		for i := 0; i < count; i++ {
			finalExpired := expiredTime
			if finalExpired == -1 {
				// 占位：按全局 DefaultCodeValidDays 算
				finalExpired = now + int64(policy.DefaultCodeValidDays*86400)
			}
			ic := &InvitationCode{
				Name:        name,
				Code:        strings.ToUpper(common.GetRandomString(12)),
				Status:      InvitationCodeStatusEnabled,
				MaxUses:     maxUses,
				CreatedBy:   user.Id,
				OwnerUserId: user.Id,
				CreatedTime: now,
				ExpiredTime: finalExpired,
				IsAdmin:     false,
			}
			if err := tx.Create(ic).Error; err != nil {
				return err
			}
			codes = append(codes, ic)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}
