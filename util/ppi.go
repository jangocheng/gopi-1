/*
	Go Language Raspberry Pi Interface
	(c) Copyright David Thorpe 2016
	All Rights Reserved

	For Licensing and Usage information, please see LICENSE.md
*/

// PPI
//
// These methods provide support for calculating the pixels-per-inch (PPI)
// of a display. The function ParseLengthString parses a string to return a
// diaganol length in inches. Typically you will call the function with one of
// the following forms:
//
//   99 in
//   99 mm
//   99 cm
//   99 x 99 in
//   99 x 99 mm
//   99 x 99 cm
//
package util /* import "github.com/djthorpe/gopi/util" */

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"fmt"
)

////////////////////////////////////////////////////////////////////////////////
// CONSTANTS

const (
	UNITS_IN_PER_MM float64 = 0.0393701
	UNITS_IN_PER_CM float64 = 0.393701
)

////////////////////////////////////////////////////////////////////////////////
// GLOBAL VARIABLES

var (
	// Syntax Error Occurred
	ErrParseError = errors.New("Syntax Error")
)

var (
	REGEXP_PPI_D  *regexp.Regexp = regexp.MustCompile("^\\s*([0-9\\.]+)\\s*(in|mm|cm)\\s*$")
	REGEXP_PPI_WH *regexp.Regexp = regexp.MustCompile("^\\s*([0-9\\.]+)\\s*(x|X)\\s*([0-9\\.]+)\\s*(in|mm|cm)\\s*$")
)

////////////////////////////////////////////////////////////////////////////////
// PUBLIC METHODS

// Return the length of the diagnol, in inches. On syntax error, ErrParseError
// is returned. On success, the number of inches on the screen diagnanol is
// returned.
func ParseLengthString(value string) (float64, error) {
	// dd (cm|mm|in) format
	match := REGEXP_PPI_D.FindStringSubmatch(value)
	if len(match) == 3 {
		return parseNumberToInches(match[1], match[2])
	}
	// dd x dd (cm|mm|in) format
	match = REGEXP_PPI_WH.FindStringSubmatch(value)
	if len(match) == 5 {
		return parseLengthsToInches(match[1], match[3], match[4])
	}
	return 0.0, ErrParseError
}

// Returns pixels per inch for a given display. Will use the screen width
// and height where the string value is provided as a length, or simply
// returns the number where value is a pure integer number.
//
//   PixelsPerInch(800,500,"8in") -> returns X
//   PixelsPerInch(800,500,"72") -> returns 72
//   PixelsPerInch(800,500,"") -> returns 0
//
//  Returns error if value cannot be decoded, or if either of the screen
// dimensions are zero.
func PixelsPerInch(w,h uint,value string) (uint, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	uint32_value, err := strconv.ParseUint(value, 32, 10)
	if err == nil {
		return uint(uint32_value), nil
	}
	fmt.Println(value)
	float64_length, err := ParseLengthString(value)
	if err == nil {
		if w == 0 || h == 0 {
			return 0, ErrParseError
		}
		ppi := math.Sqrt(math.Pow(float64(w), 2) + math.Pow(float64(h), 2)) / float64_length
		return uint(ppi), nil
	}
	return 0, err
}

////////////////////////////////////////////////////////////////////////////////
// PRIVATE METHODS

// return diaganol length and error given two numbers in string form and units
func parseLengthsToInches(value1 string, value2 string, units string) (float64, error) {
	float1, err := parseNumberToInches(value1, units)
	if err != nil {
		return float1, err
	}
	float2, err := parseNumberToInches(value2, units)
	if err != nil {
		return float2, err
	}
	return math.Sqrt(math.Pow(float1, 2) + math.Pow(float2, 2)), nil
}

// return multiplier to convert a value to inches
func multiplierForUnit(units string) float64 {
	switch units {
	case "cm":
		return UNITS_IN_PER_CM
	case "mm":
		return UNITS_IN_PER_MM
	case "in":
		return 1.0
	default:
		return 0.0
	}
}

// return inches given a number in string form and the units
func parseNumberToInches(value string, units string) (float64, error) {
	float, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return float, ErrParseError
	}
	multipler := multiplierForUnit(units)
	if multipler == 0.0 {
		return float, ErrParseError
	}
	return float * multipler, nil
}
