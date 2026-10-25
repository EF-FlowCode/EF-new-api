package controller

import (
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting/system_setting"
)

func paymentReturnPath(suffix string) string {
	base := strings.TrimRight(system_setting.GetServerAddress(), "/")
	return base + common.LegacyConsolePath(suffix)
}
