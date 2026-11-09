// Package taipei is the owner's time: a statement's dates are Taipei's (ADR 0004), and so is a day of
// snapshots or prices.
package taipei

import "time"

// Zone is Taipei's. Taiwan has kept UTC+8 without daylight saving since 1979, and a fixed zone needs no
// time zone database in the image.
var Zone = time.FixedZone("Asia/Taipei", 8*60*60)

// Today is the date in Taipei at t, as a date without time zone.
func Today(t time.Time) time.Time {
	y, m, d := t.In(Zone).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// At is the moment a date without time zone reaches a time of day in Taipei.
func At(day time.Time, hour, minute int) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, hour, minute, 0, 0, Zone)
}
