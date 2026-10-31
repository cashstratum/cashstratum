// ckpool.log tailing: a logtail.Tailer follows the file from EOF and a
// poolevents.Classifier turns its lines into the three pool-block events
// (contract §4.2-4.4).
package main

import (
	"cashstratumapi/internal/logtail"
	"cashstratumapi/internal/poolevents"
)

// LogTailer follows ckpool.log; see NewLogTailer.
type LogTailer = logtail.Tailer

// NewLogTailer builds a tailer for the ckpool.log at path whose lines are
// classified into pool.block.submitted / pool.block / pool.block.rejected
// events delivered to onEvent. Lines that are not block events are
// dropped. onEvent may be nil.
func NewLogTailer(path string, onEvent func(event any)) *LogTailer {
	classifier := poolevents.NewClassifier(nil)
	return logtail.New(path, func(line string) {
		if evt, ok := classifier.Classify(line); ok && onEvent != nil {
			onEvent(evt)
		}
	})
}
