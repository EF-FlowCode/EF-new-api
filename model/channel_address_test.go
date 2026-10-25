package model

import "testing"

func TestClassifyChannelAddress(t *testing.T) {
	cases := map[string]string{
		"http://203.0.113.10:8080":      ChannelAddressIP,
		"https://1.2.3.4":               ChannelAddressIP,
		"10.0.0.1:3000":                 ChannelAddressIP,
		"http://[2001:db8::1]:8080/v1":  ChannelAddressIP,
		"https://relay.example.net":    ChannelAddressDomain,
		"https://api.openai.com/v1":     ChannelAddressDomain,
		"http://localhost:3000":         ChannelAddressDomain,
		"":                              ChannelAddressDomain,
		"   ":                           ChannelAddressDomain,
		"http://1.2.3.4.example.com:80": ChannelAddressDomain,
	}
	for in, want := range cases {
		if got := ClassifyChannelAddress(in); got != want {
			t.Errorf("ClassifyChannelAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeChannelAddressFilter(t *testing.T) {
	cases := map[string]string{"ip": "ip", " IP ": "ip", "domain": "domain", "all": "", "": "", "x": ""}
	for in, want := range cases {
		if got := NormalizeChannelAddressFilter(in); got != want {
			t.Errorf("NormalizeChannelAddressFilter(%q) = %q, want %q", in, got, want)
		}
	}
}
