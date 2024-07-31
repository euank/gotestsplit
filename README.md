# gotestsplit

Split a go test file into a bunch of test files.

This exists since file-based dependency tracking (i.e. bazel and friends) likes to reason about files as the unit of work, so there's some world where having a test-per-file might be better. This allows splitting some real-world file and seeing if that's actually the case or not.
