package provider

import "testing"

func TestParseSo360Response(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		phone string
		want  string
	}{
		{
			name:  "标记号码",
			phone: "13120886542",
			body:  `callback({"html":"<span class='mh-des-phone'>13120886542</span><span class='mohe-ph-mark'>广告推销</span>","query":"13120886542","type":"mobilecheck"})`,
			want:  "广告推销",
		},
		{
			name:  "普通号码",
			phone: "13245678901",
			body:  `callback({"html":"<span class='mh-des-phone'>13245678901</span>","query":"13245678901","type":"mobilecheck"})`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSo360Response([]byte(tt.body), tt.phone)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("label = %q, want %q", got, tt.want)
			}
		})
	}
}
