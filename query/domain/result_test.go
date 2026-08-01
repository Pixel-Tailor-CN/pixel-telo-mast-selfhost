package domain

import "testing"

func TestRecordIsSpam(t *testing.T) {
	record := Record{PhoneNumber: "13800138000", Source: "sogou", Confidence: 100}
	if !record.IsSpam() {
		t.Fatal("record should be spam")
	}
}
