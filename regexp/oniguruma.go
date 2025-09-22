// Package regexp implements a regular expression library using Oniguruma
package regexp

// #cgo pkg-config: oniguruma
// #include <oniguruma.h>
// #include <stdlib.h>
//
// int error_code_to_str(UChar* err_buf, int err_code, OnigErrorInfo* info) {
//     return info != NULL ? onig_error_code_to_str(err_buf, err_code, info) : onig_error_code_to_str(err_buf, err_code);
// }
import "C"
import (
	"errors"
	"fmt"
	"unsafe"
)

var (
	ErrRegexpSyntax = errors.New("syntax error")
)

type Regexp struct {
	c       C.OnigRegex
	pattern string
}

type Range struct {
	Start, End int
}

func (r Range) Len() int {
	return r.End - r.Start
}

func (r Range) Text(str string) string {
	return str[r.Start:r.End]
}

type Option C.OnigOptionType

const (
	OptionDefault                            Option = C.ONIG_OPTION_DEFAULT
	OptionNone                               Option = C.ONIG_OPTION_NONE
	OptionIgnorecase                         Option = C.ONIG_OPTION_IGNORECASE
	OptionExtend                             Option = C.ONIG_OPTION_EXTEND
	OptionMultiline                          Option = C.ONIG_OPTION_MULTILINE
	OptionSingleline                         Option = C.ONIG_OPTION_SINGLELINE
	OptionFindLongest                        Option = C.ONIG_OPTION_FIND_LONGEST
	OptionFindNotEmpty                       Option = C.ONIG_OPTION_FIND_NOT_EMPTY
	OptionNegateSingleline                   Option = C.ONIG_OPTION_NEGATE_SINGLELINE
	OptionDontCaptureGroup                   Option = C.ONIG_OPTION_DONT_CAPTURE_GROUP
	OptionCaptureGroup                       Option = C.ONIG_OPTION_CAPTURE_GROUP
	OptionNotBOL                             Option = C.ONIG_OPTION_NOTBOL
	OptionNotEOL                             Option = C.ONIG_OPTION_NOTEOL
	OptionPosixRegion                        Option = C.ONIG_OPTION_POSIX_REGION
	OptionCheckValidityOfString              Option = C.ONIG_OPTION_CHECK_VALIDITY_OF_STRING
	OptionIgnorecaseIsASCII                  Option = C.ONIG_OPTION_IGNORECASE_IS_ASCII
	OptionWordIsASCII                        Option = C.ONIG_OPTION_WORD_IS_ASCII
	OptionDigitIsASCII                       Option = C.ONIG_OPTION_DIGIT_IS_ASCII
	OptionSpaceIsASCII                       Option = C.ONIG_OPTION_SPACE_IS_ASCII
	OptionPosixIsASCII                       Option = C.ONIG_OPTION_POSIX_IS_ASCII
	OptionTextSegmentExtendedGraphemeCluster Option = C.ONIG_OPTION_TEXT_SEGMENT_EXTENDED_GRAPHEME_CLUSTER
	OptionTextSegmentWord                    Option = C.ONIG_OPTION_TEXT_SEGMENT_WORD
	OptionNotBeginString                     Option = C.ONIG_OPTION_NOT_BEGIN_STRING
	OptionNotEndString                       Option = C.ONIG_OPTION_NOT_END_STRING
	OptionNotBeginPosition                   Option = C.ONIG_OPTION_NOT_BEGIN_POSITION
	OptionCallbackEachMatch                  Option = C.ONIG_OPTION_CALLBACK_EACH_MATCH
	OptionMatchWholeString                   Option = C.ONIG_OPTION_MATCH_WHOLE_STRING
	OptionMaxbit                             Option = C.ONIG_OPTION_MAXBIT
)

var syntax = C.ONIG_SYNTAX_DEFAULT

func Compile(pattern string, option Option) (*Regexp, error) {
	r := Regexp{pattern: pattern}
	bytes := []byte(pattern)
	if len(bytes) == 0 {
		return nil, fmt.Errorf("%w: empty pattern", ErrRegexpSyntax)
	}
	start := (*C.OnigUChar)(unsafe.Pointer(&bytes[0]))
	end := (*C.OnigUChar)(unsafe.Pointer(uintptr(unsafe.Pointer(&bytes[0])) + uintptr(len(bytes))))

	var errinfo C.OnigErrorInfo

	ret := C.onig_new(&r.c, start, end, C.OnigOptionType(option), C.ONIG_ENCODING_UTF8, syntax, &errinfo)
	if ret != C.ONIG_NORMAL {
		var errBuf [C.ONIG_MAX_ERROR_MESSAGE_LEN]C.char
		C.error_code_to_str((*C.OnigUChar)(unsafe.Pointer(&errBuf[0])), ret, &errinfo)
		return nil, fmt.Errorf("%w: %s", ErrRegexpSyntax, C.GoString(&errBuf[0]))
	}

	return &r, nil
}

func (re *Regexp) Free() {
	C.onig_free(re.c)
	re.c = nil
}

func (re *Regexp) String() string {
	return re.pattern
}

func (re *Regexp) Match(text string, from int, to int, options Option) ([]Range, error) {
	if len(text) == 0 {
		return nil, nil
	}
	bytes := []byte(text)
	cpattern := (*C.OnigUChar)(unsafe.Pointer(&bytes[0]))
	start := (*C.OnigUChar)(unsafe.Pointer(uintptr(unsafe.Pointer(&bytes[0])) + uintptr(from)))
	end := (*C.OnigUChar)(unsafe.Pointer(uintptr(unsafe.Pointer(&bytes[0])) + uintptr(to)))

	region := C.onig_region_new()
	defer C.onig_region_free(region, 1)

	ret := C.onig_match(re.c, cpattern, end, start, region, C.OnigOptionType(options))
	if ret == C.ONIG_MISMATCH {
		return nil, nil
	} else if ret < 0 {
		var errBuf [C.ONIG_MAX_ERROR_MESSAGE_LEN]C.char
		C.error_code_to_str((*C.OnigUChar)(unsafe.Pointer(&errBuf[0])), ret, nil)
		return nil, fmt.Errorf("%w: %s", ErrRegexpSyntax, errors.New(C.GoString(&errBuf[0])))
	}

	groups := make([]Range, region.num_regs)
	for i := range int(region.num_regs) {
		beg := *(*C.int)(unsafe.Pointer(uintptr(unsafe.Pointer(region.beg)) + uintptr(i)*unsafe.Sizeof(*region.beg)))
		end := *(*C.int)(unsafe.Pointer(uintptr(unsafe.Pointer(region.end)) + uintptr(i)*unsafe.Sizeof(*region.end)))
		if beg == -1 || end == -1 {
			continue
		}
		groups[i] = Range{int(beg), int(end)}
	}

	return groups, nil
}
