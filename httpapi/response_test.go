package httpapi

import (
	"testing"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/phone"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
)

func TestMakeQueryResponseUsesOfficialSpamThreshold(t *testing.T) {
	for _, test := range []struct {
		confidence int64
		wantSpam   bool
	}{
		{confidence: 50, wantSpam: false},
		{confidence: 51, wantSpam: true},
	} {
		result := &domain.LookupResult{Record: &domain.Record{PhoneNumber: "13800138000", Confidence: test.confidence}}
		response := makeQueryResponse(result, nil)
		if response.IsSpam != test.wantSpam {
			t.Fatalf("confidence = %d, is_spam = %t", test.confidence, response.IsSpam)
		}
	}
}

func TestMakeQueryResponseMapsPhoneData(t *testing.T) {
	result := &domain.LookupResult{Record: &domain.Record{PhoneNumber: "13800138000"}}
	response := makeQueryResponse(result, &phone.Record{CardType: "中国移动", Province: "浙江", City: "杭州"})
	if response.Data == nil || response.Data.CardType != "中国移动" || response.Data.Province != "浙江" || response.Data.City != "杭州" {
		t.Fatalf("data = %#v", response.Data)
	}
}
