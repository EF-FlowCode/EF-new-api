package claude

func NormalizeCacheCreationSplit(totalTokens int, tokens5m int, tokens1h int) (int, int) {
	remainder := max(totalTokens-tokens5m-tokens1h, 0)
	return tokens5m + remainder, tokens1h
}
