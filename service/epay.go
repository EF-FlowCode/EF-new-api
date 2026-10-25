package service

import (
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/setting/system_setting"
)

func GetCallbackAddress() string {
	return GetCallbackAddressWithConfig(operation_setting.GetEpayConfig())
}

func GetCallbackAddressWithConfig(config operation_setting.EpayConfig) string {
	if config.CustomCallbackAddress == "" {
		return system_setting.GetServerAddress()
	}
	return config.CustomCallbackAddress
}
