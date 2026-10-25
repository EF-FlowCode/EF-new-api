package operation_setting

func AutomaticDisableKeywordsToString() string {
	return GetChannelFailurePolicy().AutomaticDisableKeywordsString()
}

func AutomaticDisableKeywordsFromString(s string) {
	_ = UpdateChannelFailurePolicy(map[string]string{"AutomaticDisableKeywords": s})
}
