package phone

import "testing"

func TestNewFinderLoadsEmbeddedData(t *testing.T) {
	finder := NewFinder()
	if finder == nil || len(finder.content) < headerLength || finder.firstOffset <= 0 {
		t.Fatalf("finder = %#v", finder)
	}
}

func TestFinderFind(t *testing.T) {
	finder := newTestFinder(t, "1380013", "浙江|杭州|310000|0571", cardCMCC)
	got := finder.Find("13800138000")
	if got == nil {
		t.Fatal("expected phone data")
	}
	if got.Province != "浙江" || got.City != "杭州" || got.CardType != "中国移动" {
		t.Fatalf("record = %#v", got)
	}
}

func TestFinderReturnsNilForInvalidOrMissingPhone(t *testing.T) {
	finder := newTestFinder(t, "1380013", "浙江|杭州|310000|0571", cardCMCC)
	for _, phoneNumber := range []string{"", "123456", "abcdefg1234", "13900138000"} {
		if got := finder.Find(phoneNumber); got != nil {
			t.Fatalf("phone %q returned %#v", phoneNumber, got)
		}
	}
}

func TestFinderUsesUnknownCardType(t *testing.T) {
	finder := newTestFinder(t, "1380013", "浙江|杭州|310000|0571", 99)
	got := finder.Find("13800138000")
	if got == nil || got.CardType != "未知运营商" {
		t.Fatalf("record = %#v", got)
	}
}

func newTestFinder(t *testing.T, prefix, payload string, cardType byte) *Finder {
	t.Helper()
	prefixNumber, err := parseUint32(prefix)
	if err != nil {
		t.Fatal(err)
	}
	content := make([]byte, headerLength)
	content = append(content, []byte(payload)...)
	content = append(content, 0)
	firstOffset := len(content)
	writeInt32(content[intLength:intLength*2], int32(firstOffset))
	content = append(content, make([]byte, phoneIndexLength)...)
	writeInt32(content[firstOffset:firstOffset+intLength], int32(prefixNumber))
	writeInt32(content[firstOffset+intLength:firstOffset+intLength*2], headerLength)
	content[firstOffset+intLength*2] = cardType
	return &Finder{content: content, firstOffset: int32(firstOffset)}
}

func writeInt32(destination []byte, value int32) {
	destination[0] = byte(value)
	destination[1] = byte(value >> 8)
	destination[2] = byte(value >> 16)
	destination[3] = byte(value >> 24)
}
