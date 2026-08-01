// Package phone 提供内嵌号码段归属地数据查询。
package phone

import (
	"bytes"
	_ "embed"
	"errors"
	"log/slog"
)

const (
	cardCMCC byte = iota + 0x01
	cardCUCC
	cardCTCC
	cardCTCCVirtual
	cardCUCCVirtual
	cardCMCCVirtual
	cardCBCC
	cardCBCCVirtual
	intLength        = 4
	charLength       = 1
	headerLength     = 8
	phoneIndexLength = 9
)

// Record 表示号码段对应的归属地和运营商信息。
type Record struct {
	PhoneNumber string
	Province    string
	City        string
	ZipCode     string
	AreaZone    string
	CardType    string
}

// Finder 从内嵌 phone.dat 中执行二分查询。
type Finder struct {
	content     []byte
	firstOffset int32
}

var (
	//go:embed phone.dat
	phoneDataContent []byte

	cardTypeNames = map[byte]string{
		cardCMCC:        "中国移动",
		cardCUCC:        "中国联通",
		cardCTCC:        "中国电信",
		cardCTCCVirtual: "中国电信虚拟运营商",
		cardCUCCVirtual: "中国联通虚拟运营商",
		cardCMCCVirtual: "中国移动虚拟运营商",
		cardCBCC:        "中国广电",
		cardCBCCVirtual: "中国广电虚拟运营商",
	}
)

// NewFinder 创建使用内嵌数据的归属地查询器。
func NewFinder() *Finder {
	finder := &Finder{content: phoneDataContent}
	if len(phoneDataContent) < headerLength {
		slog.Error("embedded phone data is invalid", "length", len(phoneDataContent))
		return finder
	}
	finder.firstOffset = readInt32(phoneDataContent[intLength : intLength*2])
	return finder
}

// Find 查询号码前七位对应的归属地记录。
func (f *Finder) Find(phoneNumber string) (record *Record) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("phone data lookup failed", "error_type", "panic")
			record = nil
		}
	}()

	if len(phoneNumber) < 7 || len(phoneNumber) > 11 || len(f.content) < headerLength {
		return nil
	}
	prefix, err := parseUint32(phoneNumber[:7])
	if err != nil {
		return nil
	}

	left := int32(0)
	right := (int32(len(f.content)) - f.firstOffset) / phoneIndexLength
	for left <= right {
		middle := (left + right) / 2
		offset := f.firstOffset + middle*phoneIndexLength
		if offset < 0 || offset+phoneIndexLength > int32(len(f.content)) {
			return nil
		}
		currentPrefix := readInt32(f.content[offset : offset+intLength])
		recordOffset := readInt32(f.content[offset+intLength : offset+intLength*2])
		cardType := f.content[offset+intLength*2 : offset+intLength*2+charLength][0]
		switch {
		case currentPrefix > int32(prefix):
			right = middle - 1
		case currentPrefix < int32(prefix):
			left = middle + 1
		default:
			return f.readRecord(phoneNumber, recordOffset, cardType)
		}
	}
	return nil
}

func (f *Finder) readRecord(phoneNumber string, offset int32, cardType byte) *Record {
	if offset < 0 || offset >= int32(len(f.content)) {
		return nil
	}
	payload := f.content[offset:]
	end := bytes.IndexByte(payload, 0)
	if end < 0 {
		return nil
	}
	fields := bytes.Split(payload[:end], []byte("|"))
	if len(fields) < 4 {
		return nil
	}
	cardName, ok := cardTypeNames[cardType]
	if !ok {
		cardName = "未知运营商"
	}
	return &Record{
		PhoneNumber: phoneNumber,
		Province:    string(fields[0]),
		City:        string(fields[1]),
		ZipCode:     string(fields[2]),
		AreaZone:    string(fields[3]),
		CardType:    cardName,
	}
}

func readInt32(value []byte) int32 {
	if len(value) < intLength {
		return 0
	}
	return int32(value[0]) | int32(value[1])<<8 | int32(value[2])<<16 | int32(value[3])<<24
}

func parseUint32(value string) (uint32, error) {
	var number uint32
	for index := range len(value) {
		char := value[index]
		if char < '0' || char > '9' {
			return 0, errors.New("invalid phone prefix")
		}
		digit := uint32(char - '0')
		if number > (^uint32(0)-digit)/10 {
			return 0, errors.New("phone prefix out of range")
		}
		number = number*10 + digit
	}
	return number, nil
}
