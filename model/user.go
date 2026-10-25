package model

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/setting/operation_setting"

	gopool "eflow.local/new-api/internal/drainpool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const UserNameMaxLength = 20

// User if you add sensitive fields, don't forget to clean them in setupLogin function.
// Otherwise, the sensitive information will be saved on local storage in plain text!
type User struct {
	Id                      int            `json:"id"`
	Username                string         `json:"username" gorm:"unique;index" validate:"max=20"`
	Password                string         `json:"password" gorm:"not null;" validate:"min=8,max=20"`
	OriginalPassword        string         `json:"original_password" gorm:"-:all"` // this field is only for Password change verification, don't save it to database!
	DisplayName             string         `json:"display_name" gorm:"index" validate:"max=20"`
	Role                    int            `json:"role" gorm:"type:int;default:1"`   // admin, common
	Status                  int            `json:"status" gorm:"type:int;default:1"` // enabled, disabled
	Email                   string         `json:"email" gorm:"index" validate:"max=50"`
	GitHubId                string         `json:"github_id" gorm:"column:github_id;index"`
	DiscordId               string         `json:"discord_id" gorm:"column:discord_id;index"`
	OidcId                  string         `json:"oidc_id" gorm:"column:oidc_id;index"`
	WeChatId                string         `json:"wechat_id" gorm:"column:wechat_id;index"`
	TelegramId              string         `json:"telegram_id" gorm:"column:telegram_id;index"`
	VerificationCode        string         `json:"verification_code" gorm:"-:all"`                         // this field is only for Email verification, don't save it to database!
	AccessToken             *string        `json:"-" gorm:"type:char(32);column:access_token;uniqueIndex"` // this token is for system management
	Quota                   int            `json:"quota" gorm:"type:int;default:0"`
	UsedQuota               int            `json:"used_quota" gorm:"type:int;default:0;column:used_quota"` // used quota
	RequestCount            int            `json:"request_count" gorm:"type:int;default:0;"`               // request number
	CreatedTime             int64          `json:"created_time" gorm:"bigint"`
	Group                   string         `json:"group" gorm:"type:varchar(64);default:'default'"`
	AdditionalCustomerTiers GroupList      `json:"additional_customer_tiers" gorm:"type:text;column:additional_customer_tiers"`
	AffCode                 string         `json:"aff_code" gorm:"type:varchar(32);column:aff_code;uniqueIndex"`
	AffCount                int            `json:"aff_count" gorm:"type:int;default:0;column:aff_count"`
	AffQuota                int            `json:"aff_quota" gorm:"type:int;default:0;column:aff_quota"`           // 邀请剩余额度
	AffHistoryQuota         int            `json:"aff_history_quota" gorm:"type:int;default:0;column:aff_history"` // 邀请历史额度
	InviterId               int            `json:"inviter_id" gorm:"type:int;column:inviter_id;index"`
	DeletedAt               gorm.DeletedAt `gorm:"index"`
	LinuxDOId               string         `json:"linux_do_id" gorm:"column:linux_do_id;index"`
	Setting                 string         `json:"setting" gorm:"type:text;column:setting"`
	InvitationCode          string         `json:"invitation_code" gorm:"-:all"`
	Remark                  string         `json:"remark,omitempty" gorm:"type:varchar(255)" validate:"max=255"`
	StripeCustomer          string         `json:"stripe_customer" gorm:"type:varchar(64);column:stripe_customer;index"`
	CreatedAt               int64          `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	LastLoginAt             int64          `json:"last_login_at" gorm:"default:0;column:last_login_at"`
	// RiskWarningPendingAt is the unix timestamp of the most recent enforce-mode
	// block/observe decision recorded against this user. Login flow shows a
	// vague "your account triggered platform protection" modal when non-zero.
	// Acknowledging zeroes it; a fresh decision against any group refreshes it.
	RiskWarningPendingAt int64 `json:"risk_warning_pending_at" gorm:"bigint;default:0;index"`

	// Enforcement layer counters. Atomic increment is done inside a row-level
	// FOR UPDATE transaction (see IncrementEnforcementHit) so concurrent hits
	// on the same user can never lose an increment. Window semantics: when
	// (now - WindowStart) >= CountWindowHours both source counters are reset.
	EnforcementHitCountRisk          int   `json:"enforcement_hit_count_risk" gorm:"default:0;index"`
	EnforcementHitCountModeration    int   `json:"enforcement_hit_count_moderation" gorm:"default:0;index"`
	EnforcementWindowStartAt         int64 `json:"enforcement_window_start_at" gorm:"bigint;default:0"`
	EnforcementLastHitAt             int64 `json:"enforcement_last_hit_at" gorm:"bigint;default:0;index"`
	EnforcementEmailWindowStartAt    int64 `json:"enforcement_email_window_start_at" gorm:"bigint;default:0"`
	EnforcementEmailCountInWindow    int   `json:"enforcement_email_count_in_window" gorm:"default:0"`
	EnforcementBanEmailWindowStartAt int64 `json:"enforcement_ban_email_window_start_at" gorm:"bigint;default:0"`
	EnforcementBanEmailCountInWindow int   `json:"enforcement_ban_email_count_in_window" gorm:"default:0"`
	EnforcementAutoBannedAt          int64 `json:"enforcement_auto_banned_at" gorm:"bigint;default:0;index"`
}

func (user *User) ToBaseUser() *UserBase {
	cache := &UserBase{
		Id:                      user.Id,
		Group:                   user.Group,
		AdditionalCustomerTiers: user.AdditionalCustomerTiers,
		Quota:                   user.Quota,
		Status:                  user.Status,
		Role:                    user.Role,
		Username:                user.Username,
		Setting:                 user.Setting,
		Email:                   user.Email,
		CacheSchema:             userCacheSchemaVersion,
	}
	return cache
}

func (user *User) GetAccessToken() string {
	if user.AccessToken == nil {
		return ""
	}
	return *user.AccessToken
}

func (user *User) SetAccessToken(token string) {
	user.AccessToken = &token
}

// SetLegacyUserAccessToken 仅轮换旧版控制台访问令牌（过渡期后停用），避免把过期的用户快照写回
// 并覆盖并发发生的额度、返利或资料变更。
func SetLegacyUserAccessToken(id int, token string) error {
	if id == 0 {
		return errors.New("id 为空！")
	}
	result := DB.Model(&User{}).Where("id = ?", id).Update("access_token", token)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (user *User) GetSetting() dto.UserSetting {
	setting := dto.UserSetting{}
	if user.Setting != "" {
		err := common.Unmarshal([]byte(user.Setting), &setting)
		if err != nil {
			common.SysLog("failed to unmarshal setting: " + err.Error())
		}
	}
	return setting
}

func (user *User) SetSetting(setting dto.UserSetting) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog("failed to marshal setting: " + err.Error())
		return
	}
	user.Setting = string(settingBytes)
}

func UpdateUserSetting(userId int, setting dto.UserSetting) error {
	if userId == 0 {
		return errors.New("id 为空！")
	}
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		return err
	}
	settingValue := string(settingBytes)
	if err = DB.Model(&User{}).Where("id = ?", userId).Update("setting", settingValue).Error; err != nil {
		return err
	}
	return updateUserSettingCache(userId, settingValue)
}

// userBindColumns 是第三方账号绑定列白名单，防止专用更新入口被用于修改
// 角色、状态、分组或其他用户字段。
var userBindColumns = map[string]bool{
	"github_id":   true,
	"discord_id":  true,
	"oidc_id":     true,
	"linux_do_id": true,
	"wechat_id":   true,
	"telegram_id": true,
}

// UpdateUserBindColumn 只更新指定的第三方账号绑定列。
// 绑定流程不能使用“读取完整用户快照 -> 修改一个字段 -> 整体更新”，否则
// 读取期间发生的封禁、降权或分组变更可能被旧快照覆盖。
func UpdateUserBindColumn(userId int, column string, value string) error {
	if userId <= 0 {
		return errors.New("id 为空！")
	}
	if !userBindColumns[column] {
		return fmt.Errorf("invalid user bind column: %s", column)
	}
	return DB.Model(&User{}).Where("id = ?", userId).Update(column, value).Error
}

// 根据用户角色生成默认的边栏配置
func generateDefaultSidebarConfigForRole(userRole int) string {
	defaultConfig := map[string]any{}

	// 聊天区域 - 所有用户都可以访问
	defaultConfig["chat"] = map[string]any{
		"enabled":    true,
		"playground": true,
		"chat":       true,
	}

	// 控制台区域 - 所有用户都可以访问
	defaultConfig["console"] = map[string]any{
		"enabled":    true,
		"detail":     true,
		"token":      true,
		"log":        true,
		"midjourney": true,
		"task":       true,
		"ticket":     true,
	}

	// 个人中心区域 - 所有用户都可以访问
	defaultConfig["personal"] = map[string]any{
		"enabled":  true,
		"topup":    true,
		"personal": true,
	}

	// 管理员区域 - 根据角色决定
	if userRole == common.RoleAdminUser {
		// 管理员可以访问管理员区域，但不能访问系统设置
		defaultConfig["admin"] = map[string]any{
			"enabled":         true,
			"channel":         true,
			"models":          true,
			"risk":            true,
			"redemption":      true,
			"invitation_code": true,
			"user":            true,
			"ticket_admin":    true,
			"setting":         false, // 管理员不能访问系统设置
		}
	} else if userRole == common.RoleRootUser {
		// 超级管理员可以访问所有功能
		defaultConfig["admin"] = map[string]any{
			"enabled":         true,
			"channel":         true,
			"models":          true,
			"risk":            true,
			"redemption":      true,
			"invitation_code": true,
			"user":            true,
			"ticket_admin":    true,
			"setting":         true,
		}
	}
	// 普通用户不包含admin区域

	// 转换为JSON字符串
	configBytes, err := common.Marshal(defaultConfig)
	if err != nil {
		common.SysLog("生成默认边栏配置失败: " + err.Error())
		return ""
	}

	return string(configBytes)
}

// CheckUserExistOrDeleted check if user exist or deleted, if not exist, return false, nil, if deleted or exist, return true, nil
func CheckUserExistOrDeleted(username string, email string) (bool, error) {
	var user User

	// err := DB.Unscoped().First(&user, "username = ? or email = ?", username, email).Error
	// check email if empty
	var err error
	email = NormalizeEmail(email)
	if email == "" {
		err = DB.Unscoped().First(&user, "username = ?", username).Error
	} else {
		err = DB.Unscoped().First(&user, "username = ? or LOWER(email) = ?", username, email).Error
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// not exist, return false, nil
			return false, nil
		}
		// other error, return false, err
		return false, err
	}
	// exist, return true, nil
	return true, nil
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func emailQuery(tx *gorm.DB, email string) *gorm.DB {
	if tx == nil {
		tx = DB
	}
	return tx.Unscoped().Model(&User{}).Where("LOWER(email) = ?", NormalizeEmail(email))
}

func CountUsersByEmail(email string) (int64, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return 0, nil
	}
	var count int64
	err := emailQuery(DB, email).Count(&count).Error
	return count, err
}

func IsEmailAvailable(email string, excludeUserID int) (bool, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return true, nil
	}
	query := emailQuery(DB, email)
	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count == 0, nil
}

func EnsureEmailAvailable(email string, excludeUserID int) error {
	available, err := IsEmailAvailable(email, excludeUserID)
	if err != nil {
		return err
	}
	if !available {
		return ErrEmailAlreadyTaken
	}
	return nil
}

// withNormalizedEmailLock serializes concurrent writers that target the same
// normalized email inside tx, so a "check then write" sequence cannot be raced
// by two transactions. It must be called inside an active transaction; the lock
// is scoped to that transaction and released on commit/rollback.
//
// MySQL 默认 REPEATABLE READ 下，锁定读会在 email 索引上取得 next-key/gap lock，
// 阻止相同规范化邮箱的并发插入。
//
// An empty email is allowed to repeat and needs no serialization.
func withNormalizedEmailLock(tx *gorm.DB, email string, fn func(tx *gorm.DB) error) error {
	email = NormalizeEmail(email)
	if email == "" {
		return fn(tx)
	}
	var ids []int
	if err := tx.Model(&User{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").
		Where("email = ?", email).
		Find(&ids).Error; err != nil {
		return err
	}
	return fn(tx)
}

func GetMaxUserId() int {
	var user User
	DB.Unscoped().Last(&user)
	return user.Id
}

// UserSortOptions 沿用 LogSortOptions / TokenSortOptions / RedemptionSortOptions
// 的白名单模式。
type UserSortOptions struct {
	SortBy    string
	SortOrder string
}

var userSortColumns = map[string]bool{
	"id":         true,
	"username":   true,
	"quota":      true,
	"status":     true,
	"role":       true,
	"created_at": true,
}

func NewUserSortOptions(sortBy string, sortOrder string) UserSortOptions {
	by := strings.ToLower(strings.TrimSpace(sortBy))
	order := strings.ToLower(strings.TrimSpace(sortOrder))
	if !userSortColumns[by] {
		by = ""
		order = ""
	} else if order != "asc" {
		order = "desc"
	}
	return UserSortOptions{SortBy: by, SortOrder: order}
}

func (o UserSortOptions) Apply(tx *gorm.DB) *gorm.DB {
	if o.SortBy == "" {
		return tx.Order("id desc")
	}
	return tx.Order(o.SortBy + " " + o.SortOrder + ", id desc")
}

func GetAllUsers(pageInfo *common.PageInfo, sortOpts UserSortOptions, roles []int, statuses []int, group string) (users []*User, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// eflow: 角色 / 状态 / 分组三个筛选都接到后端，跨页正确。
	// - role 列不需要额外包装
	// - status 同 role
	// - group 是 SQL 保留字，必须使用 MySQL 反引号包装
	base := tx.Unscoped().Model(&User{})
	if len(roles) > 0 {
		base = base.Where("role IN ?", roles)
	}
	if len(statuses) > 0 {
		base = base.Where("status IN ?", statuses)
	}
	if group != "" {
		base = base.Where(commonGroupCol+" = ?", group)
	}

	// Get total count within transaction
	err = base.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated users within same transaction
	err = sortOpts.Apply(base).Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Omit("password", "access_token").Find(&users).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

// eflow: searchId / searchUsername / searchEmail 是用户列表「三个独立搜索框」专用，
// 各自独立、彼此及与 keyword/group/role/status 全部取 AND，不再跨字段 OR。
// 旧的单 keyword 入参保留（跨字段 OR），与三字段共存——前端现已只用三字段。
func SearchUsers(keyword string, group string, roles []int, statuses []int, startIdx int, num int, sortOpts UserSortOptions, searchId string, searchUsername string, searchEmail string) ([]*User, int64, error) {
	var users []*User
	var total int64
	var err error

	// 开始事务
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 构建基础查询
	query := tx.Unscoped().Model(&User{})

	// eflow: 旧的单关键字搜索（跨字段 OR）。仅当 keyword 非空才挂——否则空 keyword
	// 会变成 LIKE '%%'，而 email 为 NULL 的用户在 `email LIKE '%%'` 下结果为 NULL
	// （即被排除），单按 ID/用户名 搜索时会莫名漏掉没填邮箱的用户。
	if keyword != "" {
		likeCondition := "username LIKE ? OR email LIKE ? OR display_name LIKE ?"
		likeArgs := []any{"%" + keyword + "%", "%" + keyword + "%", "%" + keyword + "%"}
		// 尝试将关键字转换为整数ID：数字关键字同时精确匹配 ID
		if keywordInt, convErr := strconv.Atoi(keyword); convErr == nil {
			likeCondition = "id = ? OR " + likeCondition
			likeArgs = append([]any{keywordInt}, likeArgs...)
		}
		query = query.Where("("+likeCondition+")", likeArgs...)
	}

	// eflow: 字段级精确筛选（用户列表三个独立搜索框 ID / 用户名 / 邮箱）。
	// 三者各自独立，彼此及与 keyword/group/role/status 全部取 AND，互不串字段。
	if searchId != "" {
		// 非数字的 ID 输入视为不挂该条件（前端已限制为数字输入）
		if idInt, convErr := strconv.Atoi(searchId); convErr == nil {
			query = query.Where("id = ?", idInt)
		}
	}
	if searchUsername != "" {
		// 「用户名」框同时覆盖 username 与 display_name（姓名）
		query = query.Where("(username LIKE ? OR display_name LIKE ?)",
			"%"+searchUsername+"%", "%"+searchUsername+"%")
	}
	if searchEmail != "" {
		query = query.Where("email LIKE ?", "%"+searchEmail+"%")
	}

	if group != "" {
		query = query.Where(commonGroupCol+" = ?", group)
	}

	// eflow: 角色 / 状态筛选——独立于 keyword/group，与上面已挂的 WHERE 取 AND
	if len(roles) > 0 {
		query = query.Where("role IN ?", roles)
	}
	if len(statuses) > 0 {
		query = query.Where("status IN ?", statuses)
	}

	// 获取总数
	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 获取分页数据
	err = sortOpts.Apply(query.Omit("password", "access_token")).Limit(num).Offset(startIdx).Find(&users).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 提交事务
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

// GetDistinctUserGroups 返回 users 表里实际存在过的所有 group 值（去重 + 排序）。
// 区别于 ratio_setting.GetGroupRatioCopy()——后者是 admin 在「系统设置→分组倍率」
// 里维护的"应该有"的分组配置，但 users.group 字段不强制在那份名单里
// （历史裸 SQL 改过、倍率配置删掉但用户没迁等场景，会出现 users 里有 worker 但
// 倍率配置里没有的情况）。用户列表筛选下拉要展示真实分布，必须直查这一列。
//
// 返回切片不含空串。group 是 SQL 保留字，必须使用 commonGroupCol 包装。
func GetDistinctUserGroups() ([]string, error) {
	// eflow: 直查 users.group 列实际出现过的所有取值。
	// commonGroupCol 负责包装 group 关键字。GORM 的 Distinct + Pluck 链
	// 在某些版本会把 SELECT 拼成两次（既加 Distinct 又被 Pluck 重写 SELECT），改用 Raw
	// 拼一句 SQL 最稳。包含已禁用账号（status=2）：admin 在筛选下拉里要能选到，
	// 用来快速找到"被禁用且属于某个分组"的用户。已 hard delete 的用户 GORM 会带 deleted_at IS NOT NULL
	// 自动过滤，这里不加 Unscoped（按软删语义，已删的就别在筛选下拉里出现）。
	var groups []string
	sqlStr := "SELECT DISTINCT " + commonGroupCol + " FROM users WHERE " +
		commonGroupCol + " <> '' AND deleted_at IS NULL ORDER BY " + commonGroupCol + " ASC"
	if err := DB.Raw(sqlStr).Scan(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}

// TicketStaffUser 是工单分配候选的精简视图：字段仅包含前端下拉/徽章所需的内容。
// 不暴露敏感字段（password / access_token / setting 等）。
type TicketStaffUser struct {
	Id          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Role        int    `json:"role"`
	Group       string `json:"group" gorm:"column:group"`
}

// GetTicketStaffUsers 返回所有可以处理工单的账号（客服 + 管理员 + 超级管理员），
// 仅限已启用状态。列表按角色降序 + Id 升序排序，便于前端分组渲染。
//
// 用 commonGroupCol 包装 MySQL 的 group 关键字。
func GetTicketStaffUsers() ([]*TicketStaffUser, error) {
	var users []*TicketStaffUser
	err := DB.Table("users").
		Select("id, username, display_name, email, role, "+commonGroupCol).
		Where("role >= ? AND status = ?", common.RoleCustomerServiceUser, common.UserStatusEnabled).
		Order("role desc, id asc").
		Scan(&users).Error
	if err != nil {
		return nil, err
	}
	return users, nil
}

func GetUserById(id int, selectAll bool) (*User, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	user := User{Id: id}
	var err error = nil
	if selectAll {
		err = DB.First(&user, "id = ?", id).Error
	} else {
		err = DB.Omit("password", "access_token").First(&user, "id = ?", id).Error
	}
	return &user, err
}

func GetUserIdByAffCode(affCode string) (int, error) {
	if affCode == "" {
		return 0, errors.New("affCode 为空！")
	}
	var user User
	err := DB.Select("id").First(&user, "aff_code = ?", affCode).Error
	return user.Id, err
}

func DeleteUserById(id int) (err error) {
	if id == 0 {
		return errors.New("id 为空！")
	}
	user := User{Id: id}
	return user.Delete()
}

func HardDeleteUserById(id int) error {
	if id == 0 {
		return errors.New("id 为空！")
	}
	user := User{Id: id}
	return user.HardDelete()
}

func inviteUser(inviterId int, quotaForInviter int) error {
	result := DB.Model(&User{}).Where("id = ?", inviterId).Updates(map[string]any{
		"aff_count":   gorm.Expr("aff_count + ?", 1),
		"aff_quota":   gorm.Expr("aff_quota + ?", quotaForInviter),
		"aff_history": gorm.Expr("aff_history + ?", quotaForInviter),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (user *User) TransferAffQuotaToQuota(quota int) error {
	// 检查quota是否小于最小额度
	quotaPerUnit := common.GetQuotaPerUnit()
	if float64(quota) < quotaPerUnit {
		return fmt.Errorf("转移额度最小为%s！", logger.LogQuota(int(quotaPerUnit)))
	}

	// 开始数据库事务
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer tx.Rollback() // 确保在函数退出时事务能回滚

	// 加锁查询用户以确保数据一致性
	err := lockForUpdate(tx).First(user, user.Id).Error
	if err != nil {
		return err
	}

	// 再次检查用户的AffQuota是否足够
	if user.AffQuota < quota {
		return errors.New("邀请额度不足！")
	}

	// 更新用户额度
	user.AffQuota -= quota
	user.Quota += quota

	// 保存用户状态
	if err := tx.Save(user).Error; err != nil {
		return err
	}

	// 提交事务
	return tx.Commit().Error
}

func (user *User) prepareForInsert(tx *gorm.DB) error {
	user.Email = NormalizeEmail(user.Email)
	if err := ensureEmailAvailableWithTx(tx, user.Email, 0); err != nil {
		return err
	}
	if user.Password == "" {
		return nil
	}
	var err error
	user.Password, err = common.Password2Hash(user.Password)
	return err
}

// BindEmailToUser atomically checks email availability and assigns it to the
// user, serializing concurrent binds of the same email so two accounts cannot
// end up sharing one address. The email is normalized before check and store.
func BindEmailToUser(user *User, email string) error {
	email = NormalizeEmail(email)
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return withNormalizedEmailLock(tx, email, func(tx *gorm.DB) error {
			if err := ensureEmailAvailableWithTx(tx, email, user.Id); err != nil {
				return err
			}
			user.Email = email
			// eflow: 无 UpdateWithTx；绑定只改 email 列，tx 内单列更新即可
			// （避免全行 Updates 触碰 quota 等字段），缓存由 commit 后 updateUserCache 刷新。
			return tx.Model(user).Update("email", email).Error
		})
	}); err != nil {
		return err
	}
	return updateUserCache(*user)
}

func ensureEmailAvailableWithTx(tx *gorm.DB, email string, excludeUserID int) error {
	email = NormalizeEmail(email)
	if email == "" {
		return nil
	}
	query := emailQuery(tx, email)
	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrEmailAlreadyTaken
	}
	return nil
}

func (user *User) Insert(inviterId int) error {
	quotaSettings := common.GetUserQuotaSettings()
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return withNormalizedEmailLock(tx, user.Email, func(tx *gorm.DB) error {
			if err := user.prepareForInsert(tx); err != nil {
				return err
			}
			user.Quota = quotaSettings.QuotaForNewUser
			user.AffCode = common.GetRandomString(4)
			if user.CreatedTime == 0 {
				user.CreatedTime = common.GetTimestamp()
			}

			// 初始化用户设置，包括默认的边栏配置
			if user.Setting == "" {
				defaultSetting := dto.UserSetting{}
				// 这里暂时不设置SidebarModules，因为需要在用户创建后根据角色设置
				user.SetSetting(defaultSetting)
			}

			return tx.Create(user).Error
		})
	}); err != nil {
		return err
	}

	// 用户创建成功后，根据角色初始化边栏配置
	// 需要重新获取用户以确保有正确的ID和Role
	var createdUser User
	if err := DB.Where("username = ?", user.Username).First(&createdUser).Error; err == nil {
		// 生成基于角色的默认边栏配置
		defaultSidebarConfig := generateDefaultSidebarConfigForRole(createdUser.Role)
		if defaultSidebarConfig != "" {
			currentSetting := createdUser.GetSetting()
			currentSetting.SidebarModules = defaultSidebarConfig
			createdUser.SetSetting(currentSetting)
			createdUser.Update(false)
			common.SysLog(fmt.Sprintf("为新用户 %s (角色: %d) 初始化边栏配置", createdUser.Username, createdUser.Role))
		}
	}

	if quotaSettings.QuotaForNewUser > 0 {
		RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("新用户注册赠送 %s", logger.LogQuota(quotaSettings.QuotaForNewUser)))
	}
	if shouldIssueAffiliateReward(inviterId, quotaSettings) {
		if quotaSettings.QuotaForInvitee > 0 {
			_ = IncreaseUserQuota(user.Id, quotaSettings.QuotaForInvitee, true)
			RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("使用邀请码赠送 %s", logger.LogQuota(quotaSettings.QuotaForInvitee)))
		}
		if quotaSettings.QuotaForInviter > 0 {
			//_ = IncreaseUserQuota(inviterId, quotaSettings.QuotaForInviter)
			RecordLog(inviterId, LogTypeSystem, fmt.Sprintf("邀请用户赠送 %s", logger.LogQuota(quotaSettings.QuotaForInviter)))
			_ = inviteUser(inviterId, quotaSettings.QuotaForInviter)
		}
	}
	return nil
}

// InsertWithTx inserts a new user within an existing transaction.
// This is used for OAuth registration where user creation and binding need to be atomic.
// Post-creation tasks (sidebar config, logs, inviter rewards) are handled after the transaction commits.
func (user *User) InsertWithTx(tx *gorm.DB, inviterId int, quotaSettings common.UserQuotaSettings) error {
	return withNormalizedEmailLock(tx, user.Email, func(tx *gorm.DB) error {
		if err := user.prepareForInsert(tx); err != nil {
			return err
		}
		user.Quota = quotaSettings.QuotaForNewUser
		user.AffCode = common.GetRandomString(4)
		if user.CreatedTime == 0 {
			user.CreatedTime = common.GetTimestamp()
		}

		// 初始化用户设置
		if user.Setting == "" {
			defaultSetting := dto.UserSetting{}
			user.SetSetting(defaultSetting)
		}

		return tx.Create(user).Error
	})
}

// FinalizeOAuthUserCreation performs post-transaction tasks for OAuth user creation.
// This should be called after the transaction commits successfully.
func (user *User) FinalizeOAuthUserCreation(inviterId int, quotaSettings common.UserQuotaSettings) {
	// 用户创建成功后，根据角色初始化边栏配置
	var createdUser User
	if err := DB.Where("id = ?", user.Id).First(&createdUser).Error; err == nil {
		defaultSidebarConfig := generateDefaultSidebarConfigForRole(createdUser.Role)
		if defaultSidebarConfig != "" {
			currentSetting := createdUser.GetSetting()
			currentSetting.SidebarModules = defaultSidebarConfig
			createdUser.SetSetting(currentSetting)
			createdUser.Update(false)
			common.SysLog(fmt.Sprintf("为新用户 %s (角色: %d) 初始化边栏配置", createdUser.Username, createdUser.Role))
		}
	}

	if quotaSettings.QuotaForNewUser > 0 {
		RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("新用户注册赠送 %s", logger.LogQuota(quotaSettings.QuotaForNewUser)))
	}
	if shouldIssueAffiliateReward(inviterId, quotaSettings) {
		if quotaSettings.QuotaForInvitee > 0 {
			_ = IncreaseUserQuota(user.Id, quotaSettings.QuotaForInvitee, true)
			RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("使用邀请码赠送 %s", logger.LogQuota(quotaSettings.QuotaForInvitee)))
		}
		if quotaSettings.QuotaForInviter > 0 {
			RecordLog(inviterId, LogTypeSystem, fmt.Sprintf("邀请用户赠送 %s", logger.LogQuota(quotaSettings.QuotaForInviter)))
			_ = inviteUser(inviterId, quotaSettings.QuotaForInviter)
		}
	}
}

func shouldIssueAffiliateReward(inviterId int, settings common.UserQuotaSettings) bool {
	return inviterId != 0 && settings.AffiliateEnabled && operation_setting.IsPaymentComplianceConfirmed()
}

func (user *User) Update(updatePassword bool) error {
	var err error
	if updatePassword {
		user.Password, err = common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	}
	newUser := *user
	current := User{}
	if err = DB.First(&current, user.Id).Error; err != nil {
		return err
	}
	// eflow/上游安全修复：Omit quota 类字段，防用户经资料更新接口篡改自己额度。
	if err = DB.Model(&current).Omit(
		"access_token",
		"quota",
		"used_quota",
		"request_count",
		"aff_count",
		"aff_quota",
		"aff_history",
		"additional_customer_tiers",
	).Updates(newUser).Error; err != nil {
		return err
	}
	if err = DB.First(user, user.Id).Error; err != nil {
		return err
	}

	// Update cache
	return updateUserCache(*user)
}

func (user *User) Edit(updatePassword bool) error {
	var err error
	if updatePassword {
		user.Password, err = common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	}

	newUser := *user
	updates := map[string]any{
		"username":                  newUser.Username,
		"display_name":              newUser.DisplayName,
		"group":                     newUser.Group,
		"additional_customer_tiers": newUser.AdditionalCustomerTiers,
		"remark":                    newUser.Remark,
		"email":                     newUser.Email,
	}
	if updatePassword {
		updates["password"] = newUser.Password
	}

	current := User{}
	if err = DB.First(&current, user.Id).Error; err != nil {
		return err
	}
	if err = DB.Model(&current).Updates(updates).Error; err != nil {
		return err
	}
	if err = DB.First(user, user.Id).Error; err != nil {
		return err
	}

	// Update cache
	return updateUserCache(*user)
}

func (user *User) ClearBinding(bindingType string) error {
	if user.Id == 0 {
		return errors.New("user id is empty")
	}

	bindingColumnMap := map[string]string{
		"email":    "email",
		"github":   "github_id",
		"discord":  "discord_id",
		"oidc":     "oidc_id",
		"wechat":   "wechat_id",
		"telegram": "telegram_id",
		"linuxdo":  "linux_do_id",
	}

	column, ok := bindingColumnMap[bindingType]
	if !ok {
		return errors.New("invalid binding type")
	}

	if err := DB.Model(&User{}).Where("id = ?", user.Id).Update(column, "").Error; err != nil {
		return err
	}

	if err := DB.Where("id = ?", user.Id).First(user).Error; err != nil {
		return err
	}

	return updateUserCache(*user)
}

func (user *User) Delete() error {
	if user.Id == 0 {
		return errors.New("id 为空！")
	}
	// Deleting the account removes its access tokens; disabling keeps them.
	if err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := DeleteUserAccessTokensWithTx(tx, user.Id); err != nil {
			return err
		}
		return tx.Delete(user).Error
	}); err != nil {
		return err
	}

	// 清除缓存
	return invalidateUserCache(user.Id)
}

func (user *User) HardDelete() error {
	if user.Id == 0 {
		return errors.New("id 为空！")
	}
	var tokens []Token
	err := DB.Transaction(func(tx *gorm.DB) error {
		if common.RedisEnabled {
			if err := tx.Unscoped().Select("id", commonKeyCol).Where("user_id = ?", user.Id).Find(&tokens).Error; err != nil {
				return err
			}
		}
		if err := deleteUserAuthenticationData(tx, user.Id); err != nil {
			return err
		}
		return tx.Unscoped().Delete(user).Error
	})
	if err != nil {
		return err
	}
	if err := invalidateTokensCache(tokens); err != nil {
		common.SysError(fmt.Sprintf("failed to invalidate token cache after hard deleting user %d: %v", user.Id, err))
	}
	if err := invalidateUserCache(user.Id); err != nil {
		common.SysError(fmt.Sprintf("failed to invalidate user cache after hard deleting user %d: %v", user.Id, err))
	}
	return nil
}

func deleteUserAuthenticationData(tx *gorm.DB, userId int) error {
	for _, authenticationData := range []any{
		&TwoFABackupCode{},
		&TwoFA{},
		&PasskeyCredential{},
		&Token{},
		&UserAccessToken{},
	} {
		if err := tx.Unscoped().Where("user_id = ?", userId).Delete(authenticationData).Error; err != nil {
			return err
		}
	}
	return deleteUserOAuthBindingsByUserId(tx, userId)
}

// ValidateAndFill check password & user status
func (user *User) ValidateAndFill() (err error) {
	// When querying with struct, GORM will only query with non-zero fields,
	// that means if your field's value is 0, '', false or other zero values,
	// it won't be used to build query conditions
	password := user.Password
	username := strings.TrimSpace(user.Username)
	if username == "" || password == "" {
		return ErrUserEmptyCredentials
	}
	// find by username or email
	err = DB.Where("username = ? OR email = ?", username, username).First(user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidCredentials
		}
		return fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	if user.Password == "" {
		return ErrInvalidCredentials
	}
	okay := common.ValidatePasswordAndHash(password, user.Password)
	if !okay || user.Status != common.UserStatusEnabled {
		return ErrInvalidCredentials
	}
	return nil
}

func (user *User) FillUserById() error {
	if user.Id == 0 {
		return errors.New("id 为空！")
	}
	DB.Where(User{Id: user.Id}).First(user)
	return nil
}

func (user *User) FillUserByEmail() error {
	if user.Email == "" {
		return errors.New("email 为空！")
	}
	DB.Where(User{Email: user.Email}).First(user)
	return nil
}

func (user *User) FillUserByGitHubId() error {
	if user.GitHubId == "" {
		return errors.New("GitHub id 为空！")
	}
	DB.Where(User{GitHubId: user.GitHubId}).First(user)
	return nil
}

func (user *User) FillUserByDiscordId() error {
	if user.DiscordId == "" {
		return errors.New("discord id 为空！")
	}
	DB.Where(User{DiscordId: user.DiscordId}).First(user)
	return nil
}

func (user *User) FillUserByOidcId() error {
	if user.OidcId == "" {
		return errors.New("oidc id 为空！")
	}
	DB.Where(User{OidcId: user.OidcId}).First(user)
	return nil
}

func (user *User) FillUserByWeChatId() error {
	if user.WeChatId == "" {
		return errors.New("WeChat id 为空！")
	}
	// 未找到保持 user.Id == 0，由调用方提示账号已注销；其余数据库错误向上返回。
	err := DB.Where(User{WeChatId: user.WeChatId}).First(user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

func (user *User) FillUserByTelegramId() error {
	if user.TelegramId == "" {
		return errors.New("Telegram id 为空！")
	}
	err := DB.Where(User{TelegramId: user.TelegramId}).First(user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("该 Telegram 账户未绑定")
	}
	return err
}

func IsEmailAlreadyTaken(email string) bool {
	count, err := CountUsersByEmail(email)
	return err == nil && count > 0
}

func GetUniqueUserByEmail(email string) (*User, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return nil, ErrEmailNotFound
	}
	var users []User
	if err := DB.Where("LOWER(email) = ?", email).Limit(2).Find(&users).Error; err != nil {
		return nil, err
	}
	switch len(users) {
	case 0:
		return nil, ErrEmailNotFound
	case 1:
		return &users[0], nil
	default:
		return nil, ErrEmailAmbiguous
	}
}

// isIdentityTaken 判断第三方身份是否已被任意账号占用：存在任意匹配（含历史多账号）即占用；
// 查询失败时保守视为已占用，避免数据库异常时放行重复绑定或注册。空值不代表任何身份，保持未占用。
func isIdentityTaken(query *gorm.DB, column, value string) bool {
	if value == "" {
		return false
	}
	var count int64
	if err := query.Model(&User{}).Where(column+" = ?", value).Count(&count).Error; err != nil {
		common.SysError(fmt.Sprintf("check %s taken failed: %s", column, err.Error()))
		return true
	}
	return count > 0
}

func IsWeChatIdAlreadyTaken(wechatId string) bool {
	return isIdentityTaken(DB.Unscoped(), "wechat_id", wechatId)
}

func IsGitHubIdAlreadyTaken(githubId string) bool {
	return isIdentityTaken(DB.Unscoped(), "github_id", githubId)
}

func IsDiscordIdAlreadyTaken(discordId string) bool {
	return isIdentityTaken(DB.Unscoped(), "discord_id", discordId)
}

func IsOidcIdAlreadyTaken(oidcId string) bool {
	return isIdentityTaken(DB, "oidc_id", oidcId)
}

func IsTelegramIdAlreadyTaken(telegramId string) bool {
	return isIdentityTaken(DB.Unscoped(), "telegram_id", telegramId)
}

func ResetUserPasswordByEmail(email string, password string) error {
	if email == "" || password == "" {
		return errors.New("邮箱地址或密码为空！")
	}
	user, err := GetUniqueUserByEmail(email)
	if err != nil {
		return err
	}
	hashedPassword, err := common.Password2Hash(password)
	if err != nil {
		return err
	}
	err = DB.Model(&User{}).Where("id = ?", user.Id).Update("password", hashedPassword).Error
	return err
}

func IsAdmin(userId int) bool {
	if userId == 0 {
		return false
	}
	var user User
	err := DB.Where("id = ?", userId).Select("role").Find(&user).Error
	if err != nil {
		common.SysLog("no such user " + err.Error())
		return false
	}
	return user.Role >= common.RoleAdminUser
}

func ValidateAccessToken(token string) (*User, error) {
	if token == "" {
		return nil, nil
	}
	token = strings.Replace(token, "Bearer ", "", 1)
	user := &User{}
	err := DB.Where("access_token = ?", token).First(user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	return user, nil
}

// GetUserQuota gets quota from Redis first, falls back to DB if needed
func GetUserQuota(id int, fromDB bool) (quota int, err error) {
	if !fromDB && common.RedisEnabled {
		return getUserQuotaCache(id)
	}
	err = DB.Model(&User{}).Where("id = ?", id).Select("quota").Find(&quota).Error
	if err != nil {
		return 0, err
	}

	return quota, nil
}

func GetUserUsedQuota(id int) (quota int, err error) {
	err = DB.Model(&User{}).Where("id = ?", id).Select("used_quota").Find(&quota).Error
	return quota, err
}

func GetUserEmail(id int) (email string, err error) {
	err = DB.Model(&User{}).Where("id = ?", id).Select("email").Find(&email).Error
	return email, err
}

// GetUserGroup gets group from Redis first, falls back to DB if needed
func GetUserGroup(id int, fromDB bool) (group string, err error) {
	defer func() {
		// Update Redis cache asynchronously on successful DB read
		if shouldUpdateRedis(fromDB, err) {
			gopool.Go(func() {
				if err := updateUserGroupCache(id, group); err != nil {
					common.SysLog("failed to update user group cache: " + err.Error())
				}
			})
		}
	}()
	if !fromDB && common.RedisEnabled {
		group, err := getUserGroupCache(id)
		if err == nil {
			return group, nil
		}
		// Don't return error - fall through to DB
	}
	fromDB = true
	err = DB.Model(&User{}).Where("id = ?", id).Select(commonGroupCol).Find(&group).Error
	if err != nil {
		return "", err
	}

	return group, nil
}

// GetUserSetting gets setting from Redis first, falls back to DB if needed
func GetUserSetting(id int, fromDB bool) (settingMap dto.UserSetting, err error) {
	var setting string
	defer func() {
		// Update Redis cache asynchronously on successful DB read
		if shouldUpdateRedis(fromDB, err) {
			gopool.Go(func() {
				if err := updateUserSettingCache(id, setting); err != nil {
					common.SysLog("failed to update user setting cache: " + err.Error())
				}
			})
		}
	}()
	if !fromDB && common.RedisEnabled {
		setting, err := getUserSettingCache(id)
		if err == nil {
			return setting, nil
		}
		// Don't return error - fall through to DB
	}
	fromDB = true
	// can be nil setting
	var safeSetting sql.NullString
	err = DB.Model(&User{}).Where("id = ?", id).Select("setting").Find(&safeSetting).Error
	if err != nil {
		return settingMap, err
	}
	if safeSetting.Valid {
		setting = safeSetting.String
	} else {
		setting = ""
	}
	userBase := &UserBase{
		Setting: setting,
	}
	return userBase.GetSetting(), nil
}

func IncreaseUserQuota(id int, quota int, db bool) (err error) {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if !db && common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeUserQuota, id, quota)
		updateUserQuotaCacheAsync(id, int64(quota), "increase")
		return nil
	}
	if err = increaseUserQuota(id, quota); err != nil {
		return err
	}
	updateUserQuotaCacheAsync(id, int64(quota), "increase")
	return nil
}

func increaseUserQuota(id int, quota int) (err error) {
	err = DB.Model(&User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota + ?", quota)).Error
	if err != nil {
		return err
	}
	return err
}

func DecreaseUserQuota(id int, quota int, db bool) (err error) {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if !db && common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeUserQuota, id, -quota)
		updateUserQuotaCacheAsync(id, -int64(quota), "decrease")
		return nil
	}
	if err = decreaseUserQuota(id, quota); err != nil {
		return err
	}
	updateUserQuotaCacheAsync(id, -int64(quota), "decrease")
	return nil
}

func updateUserQuotaCacheAsync(id int, delta int64, operation string) {
	if !common.RedisEnabled {
		return
	}
	gopool.Go(func() {
		_, err := cacheApplyUserQuotaDelta(id, delta)
		if err != nil {
			common.SysLog("failed to " + operation + " user quota: " + err.Error())
		}
	})
}

func decreaseUserQuota(id int, quota int) (err error) {
	err = DB.Model(&User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota - ?", quota)).Error
	if err != nil {
		return err
	}
	return err
}

func DeltaUpdateUserQuota(id int, delta int) (err error) {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		return IncreaseUserQuota(id, delta, false)
	} else {
		return DecreaseUserQuota(id, -delta, false)
	}
}

//func GetRootUserEmail() (email string) {
//	DB.Model(&User{}).Where("role = ?", common.RoleRootUser).Select("email").Find(&email)
//	return email
//}

func GetRootUser() (user *User) {
	DB.Where("role = ?", common.RoleRootUser).First(&user)
	return user
}

func UpdateUserLastLoginAt(id int) {
	if err := DB.Model(&User{}).Where("id = ?", id).Update("last_login_at", common.GetTimestamp()).Error; err != nil {
		common.SysLog("failed to update user last_login_at: " + err.Error())
	}
}

func UpdateUserUsedQuotaAndRequestCount(id int, quota int) {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeUsedQuota, id, quota)
		addNewRecord(BatchUpdateTypeRequestCount, id, 1)
		return
	}
	updateUserUsedQuotaAndRequestCount(id, quota, 1)
}

// UpdateUserUsedQuota adjusts accumulated usage without changing request_count.
// It is used by asynchronous settlement/refund paths after the initial request
// has already incremented the request counter.
func UpdateUserUsedQuota(id int, quota int) {
	if quota == 0 {
		return
	}
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeUsedQuota, id, quota)
		return
	}
	if err := DB.Model(&User{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error; err != nil {
		common.SysLog("failed to update user used quota: " + err.Error())
	}
}

func updateUserUsedQuotaAndRequestCount(id int, quota int, count int) {
	err := DB.Model(&User{}).Where("id = ?", id).Updates(
		map[string]any{
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"request_count": gorm.Expr("request_count + ?", count),
		},
	).Error
	if err != nil {
		common.SysLog("failed to update user used quota and request count: " + err.Error())
		return
	}

	//// 更新缓存
	//if err := invalidateUserCache(id); err != nil {
	//	common.SysError("failed to invalidate user cache: " + err.Error())
	//}
}

func updateUserQuotaUsedQuotaAndRequestCount(id int, quota int, usedQuota int, requestCount int) {
	if quota == 0 && usedQuota == 0 && requestCount == 0 {
		return
	}

	err := DB.Model(&User{}).Where("id = ?", id).Updates(
		map[string]any{
			"quota":         gorm.Expr("quota + ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", usedQuota),
			"request_count": gorm.Expr("request_count + ?", requestCount),
		},
	).Error
	if err != nil {
		common.SysLog("failed to batch update user quota, used quota and request count: " + err.Error())
	}
}

func updateUserUsedQuota(id int, quota int) {
	err := DB.Model(&User{}).Where("id = ?", id).Updates(
		map[string]any{
			"used_quota": gorm.Expr("used_quota + ?", quota),
		},
	).Error
	if err != nil {
		common.SysLog("failed to update user used quota: " + err.Error())
	}
}

func updateUserRequestCount(id int, count int) {
	err := DB.Model(&User{}).Where("id = ?", id).Update("request_count", gorm.Expr("request_count + ?", count)).Error
	if err != nil {
		common.SysLog("failed to update user request count: " + err.Error())
	}
}

// GetUsernameById gets username from Redis first, falls back to DB if needed
func GetUsernameById(id int, fromDB bool) (username string, err error) {
	defer func() {
		// Update Redis cache asynchronously on successful DB read
		if shouldUpdateRedis(fromDB, err) {
			gopool.Go(func() {
				if err := updateUserNameCache(id, username); err != nil {
					common.SysLog("failed to update user name cache: " + err.Error())
				}
			})
		}
	}()
	if !fromDB && common.RedisEnabled {
		username, err := getUserNameCache(id)
		if err == nil {
			return username, nil
		}
		// Don't return error - fall through to DB
	}
	fromDB = true
	err = DB.Model(&User{}).Where("id = ?", id).Select("username").Find(&username).Error
	if err != nil {
		return "", err
	}

	return username, nil
}

func IsLinuxDOIdAlreadyTaken(linuxDOId string) bool {
	var user User
	err := DB.Unscoped().Where("linux_do_id = ?", linuxDOId).First(&user).Error
	return !errors.Is(err, gorm.ErrRecordNotFound)
}

func (user *User) FillUserByLinuxDOId() error {
	if user.LinuxDOId == "" {
		return errors.New("linux do id is empty")
	}
	err := DB.Where("linux_do_id = ?", user.LinuxDOId).First(user).Error
	return err
}

func RootUserExists() bool {
	var user User
	err := DB.Where("role = ?", common.RoleRootUser).First(&user).Error
	if err != nil {
		return false
	}
	return true
}

// ----------------------------------------------------------------------------
// Risk warning + enforcement counter helpers
// ----------------------------------------------------------------------------

func MarkUserRiskWarningPending(userID int, ts int64) error {
	if userID <= 0 || ts <= 0 {
		return nil
	}
	return DB.Model(&User{}).Where("id = ?", userID).
		Update("risk_warning_pending_at", ts).Error
}

func AckUserRiskWarningPending(userID int) error {
	if userID <= 0 {
		return nil
	}
	return DB.Model(&User{}).Where("id = ?", userID).
		Update("risk_warning_pending_at", int64(0)).Error
}

func GetUserRiskWarningPending(userID int) (int64, error) {
	if userID <= 0 {
		return 0, nil
	}
	var ts int64
	err := DB.Model(&User{}).Where("id = ?", userID).
		Select("risk_warning_pending_at").Find(&ts).Error
	return ts, err
}

// EnforcementCounterSnapshot is the per-source view used by the engine.
type EnforcementCounterSnapshot struct {
	UserID                int
	HitCountRisk          int
	HitCountModeration    int
	WindowStartAt         int64
	LastHitAt             int64
	EmailWindowStartAt    int64
	EmailCountInWindow    int
	BanEmailWindowStartAt int64
	BanEmailCountInWindow int
	AutoBannedAt          int64
	Status                int
	Username              string
	Email                 string
}

func snapshotFromUser(u *User) *EnforcementCounterSnapshot {
	return &EnforcementCounterSnapshot{
		UserID:                u.Id,
		HitCountRisk:          u.EnforcementHitCountRisk,
		HitCountModeration:    u.EnforcementHitCountModeration,
		WindowStartAt:         u.EnforcementWindowStartAt,
		LastHitAt:             u.EnforcementLastHitAt,
		EmailWindowStartAt:    u.EnforcementEmailWindowStartAt,
		EmailCountInWindow:    u.EnforcementEmailCountInWindow,
		BanEmailWindowStartAt: u.EnforcementBanEmailWindowStartAt,
		BanEmailCountInWindow: u.EnforcementBanEmailCountInWindow,
		AutoBannedAt:          u.EnforcementAutoBannedAt,
		Status:                u.Status,
		Username:              u.Username,
		Email:                 u.Email,
	}
}

const (
	enforcementSourceRisk       = "risk_distribution"
	enforcementSourceModeration = "moderation"
)

// IncrementEnforcementHit atomically rolls the window if expired and bumps
// the source counter inside a row-locked transaction. Returns the snapshot
// after the increment plus rolled=true when the window was reset.
//
// The MySQL row lock serializes concurrent counter updates for the same user.
func IncrementEnforcementHit(userID int, source string, windowSeconds int64, now int64) (*EnforcementCounterSnapshot, bool, error) {
	if userID <= 0 {
		return nil, false, errors.New("invalid user id")
	}
	var snap *EnforcementCounterSnapshot
	rolled := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var u User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", userID).First(&u).Error; err != nil {
			return err
		}
		if u.EnforcementAutoBannedAt > 0 {
			snap = snapshotFromUser(&u)
			return nil
		}
		windowStart := u.EnforcementWindowStartAt
		hitRisk := u.EnforcementHitCountRisk
		hitMod := u.EnforcementHitCountModeration
		if windowSeconds > 0 {
			expiresAt := windowStart + windowSeconds
			if windowStart == 0 || now >= expiresAt {
				windowStart = now
				hitRisk = 0
				hitMod = 0
				rolled = true
			}
		} else if windowStart == 0 {
			windowStart = now
		}
		switch source {
		case enforcementSourceRisk:
			hitRisk++
		case enforcementSourceModeration:
			hitMod++
		default:
			return fmt.Errorf("unknown enforcement source: %s", source)
		}
		if err := tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
			"enforcement_hit_count_risk":       hitRisk,
			"enforcement_hit_count_moderation": hitMod,
			"enforcement_window_start_at":      windowStart,
			"enforcement_last_hit_at":          now,
		}).Error; err != nil {
			return err
		}
		u.EnforcementHitCountRisk = hitRisk
		u.EnforcementHitCountModeration = hitMod
		u.EnforcementWindowStartAt = windowStart
		u.EnforcementLastHitAt = now
		snap = snapshotFromUser(&u)
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return snap, rolled, nil
}

// LoadEnforcementCounter is for read-only callers (overview / admin list).
// Mutators MUST use IncrementEnforcementHit instead to avoid TOCTOU.
func LoadEnforcementCounter(userID int) (*EnforcementCounterSnapshot, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	var u User
	if err := DB.Where("id = ?", userID).First(&u).Error; err != nil {
		return nil, err
	}
	return snapshotFromUser(&u), nil
}

func MarkEnforcementEmailSent(userID int, windowStartAt int64, count int) error {
	if userID <= 0 {
		return nil
	}
	return DB.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"enforcement_email_window_start_at": windowStartAt,
		"enforcement_email_count_in_window": count,
	}).Error
}

func MarkEnforcementBanEmailSent(userID int, windowStartAt int64, count int) error {
	if userID <= 0 {
		return nil
	}
	return DB.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"enforcement_ban_email_window_start_at": windowStartAt,
		"enforcement_ban_email_count_in_window": count,
	}).Error
}

func MarkEnforcementAutoBanned(userID int, ts int64) error {
	if userID <= 0 {
		return nil
	}
	if err := DB.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"status":                     common.UserStatusDisabled,
		"enforcement_auto_banned_at": ts,
	}).Error; err != nil {
		return err
	}
	InvalidateUserAuthCaches(userID)
	return nil
}

func ResetEnforcementCounter(userID int) error {
	if userID <= 0 {
		return nil
	}
	return DB.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"enforcement_hit_count_risk":            0,
		"enforcement_hit_count_moderation":      0,
		"enforcement_window_start_at":           int64(0),
		"enforcement_last_hit_at":               int64(0),
		"enforcement_email_window_start_at":     int64(0),
		"enforcement_email_count_in_window":     0,
		"enforcement_ban_email_window_start_at": int64(0),
		"enforcement_ban_email_count_in_window": 0,
		"enforcement_auto_banned_at":            int64(0),
	}).Error
}

// EnforcementCounterRow is the listing row for the admin counter view.
type EnforcementCounterRow struct {
	Id                            int    `json:"id"`
	Username                      string `json:"username"`
	Email                         string `json:"email"`
	Status                        int    `json:"status"`
	EnforcementHitCountRisk       int    `json:"enforcement_hit_count_risk"`
	EnforcementHitCountModeration int    `json:"enforcement_hit_count_moderation"`
	EnforcementWindowStartAt      int64  `json:"enforcement_window_start_at"`
	EnforcementLastHitAt          int64  `json:"enforcement_last_hit_at"`
	EnforcementAutoBannedAt       int64  `json:"enforcement_auto_banned_at"`
}

func ListEnforcementCounters(startIdx, pageSize int) ([]*EnforcementCounterRow, int64, error) {
	var rows []*EnforcementCounterRow
	var total int64
	tx := DB.Model(&User{}).
		Where("enforcement_hit_count_risk > 0 OR enforcement_hit_count_moderation > 0 OR enforcement_auto_banned_at > 0")
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := tx.Select("id, username, email, status, " +
		"enforcement_hit_count_risk, enforcement_hit_count_moderation, " +
		"enforcement_window_start_at, enforcement_last_hit_at, enforcement_auto_banned_at").
		Order("(enforcement_hit_count_risk + enforcement_hit_count_moderation) desc, enforcement_last_hit_at desc, id asc").
		Limit(pageSize).Offset(startIdx).
		Find(&rows).Error
	return rows, total, err
}
