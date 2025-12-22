window.BENCHMARK_DATA = {
  "lastUpdate": 1766437747215,
  "repoUrl": "https://github.com/aponysus/recourse",
  "entries": {
    "Go Benchmarks": [
      {
        "commit": {
          "author": {
            "name": "aponysus",
            "username": "aponysus",
            "email": "me@joshuasorrell.com"
          },
          "committer": {
            "name": "aponysus",
            "username": "aponysus",
            "email": "me@joshuasorrell.com"
          },
          "id": "1c9d9c4e1a8684706788d9c73291d3198a00fbf8",
          "message": "Make benchmark pipeline manually triggered.",
          "timestamp": "2025-12-22T21:07:46Z",
          "url": "https://github.com/aponysus/recourse/commit/1c9d9c4e1a8684706788d9c73291d3198a00fbf8"
        },
        "date": 1766437746830,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkDoValue_SingleAttempt_Success",
            "value": 545.3,
            "unit": "ns/op\t     192 B/op\t       5 allocs/op",
            "extra": "2150360 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_SingleAttempt_Success - ns/op",
            "value": 545.3,
            "unit": "ns/op",
            "extra": "2150360 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_SingleAttempt_Success - B/op",
            "value": 192,
            "unit": "B/op",
            "extra": "2150360 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_SingleAttempt_Success - allocs/op",
            "value": 5,
            "unit": "allocs/op",
            "extra": "2150360 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_ThreeAttempts_AllFail",
            "value": 1075,
            "unit": "ns/op\t     480 B/op\t      11 allocs/op",
            "extra": "989997 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_ThreeAttempts_AllFail - ns/op",
            "value": 1075,
            "unit": "ns/op",
            "extra": "989997 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_ThreeAttempts_AllFail - B/op",
            "value": 480,
            "unit": "B/op",
            "extra": "989997 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_ThreeAttempts_AllFail - allocs/op",
            "value": 11,
            "unit": "allocs/op",
            "extra": "989997 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_WithTimelineCapture",
            "value": 3927,
            "unit": "ns/op\t    2048 B/op\t      26 allocs/op",
            "extra": "279517 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_WithTimelineCapture - ns/op",
            "value": 3927,
            "unit": "ns/op",
            "extra": "279517 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_WithTimelineCapture - B/op",
            "value": 2048,
            "unit": "B/op",
            "extra": "279517 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_WithTimelineCapture - allocs/op",
            "value": 26,
            "unit": "allocs/op",
            "extra": "279517 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_WithObserver",
            "value": 3728,
            "unit": "ns/op\t    1992 B/op\t      24 allocs/op",
            "extra": "291136 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_WithObserver - ns/op",
            "value": 3728,
            "unit": "ns/op",
            "extra": "291136 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_WithObserver - B/op",
            "value": 1992,
            "unit": "B/op",
            "extra": "291136 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_WithObserver - allocs/op",
            "value": 24,
            "unit": "allocs/op",
            "extra": "291136 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_FastPath_NoTimeline",
            "value": 547.3,
            "unit": "ns/op\t     192 B/op\t       5 allocs/op",
            "extra": "2199772 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_FastPath_NoTimeline - ns/op",
            "value": 547.3,
            "unit": "ns/op",
            "extra": "2199772 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_FastPath_NoTimeline - B/op",
            "value": 192,
            "unit": "B/op",
            "extra": "2199772 times\n2 procs"
          },
          {
            "name": "BenchmarkDoValue_FastPath_NoTimeline - allocs/op",
            "value": 5,
            "unit": "allocs/op",
            "extra": "2199772 times\n2 procs"
          },
          {
            "name": "BenchmarkRingBufferTracker_Observe",
            "value": 10.36,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "100000000 times\n2 procs"
          },
          {
            "name": "BenchmarkRingBufferTracker_Observe - ns/op",
            "value": 10.36,
            "unit": "ns/op",
            "extra": "100000000 times\n2 procs"
          },
          {
            "name": "BenchmarkRingBufferTracker_Observe - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "100000000 times\n2 procs"
          },
          {
            "name": "BenchmarkRingBufferTracker_Observe - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "100000000 times\n2 procs"
          },
          {
            "name": "BenchmarkRingBufferTracker_Snapshot",
            "value": 3663,
            "unit": "ns/op\t    8248 B/op\t       3 allocs/op",
            "extra": "320673 times\n2 procs"
          },
          {
            "name": "BenchmarkRingBufferTracker_Snapshot - ns/op",
            "value": 3663,
            "unit": "ns/op",
            "extra": "320673 times\n2 procs"
          },
          {
            "name": "BenchmarkRingBufferTracker_Snapshot - B/op",
            "value": 8248,
            "unit": "B/op",
            "extra": "320673 times\n2 procs"
          },
          {
            "name": "BenchmarkRingBufferTracker_Snapshot - allocs/op",
            "value": 3,
            "unit": "allocs/op",
            "extra": "320673 times\n2 procs"
          },
          {
            "name": "BenchmarkRingBufferTracker_ConcurrentObserve",
            "value": 19.42,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "63042367 times\n2 procs"
          },
          {
            "name": "BenchmarkRingBufferTracker_ConcurrentObserve - ns/op",
            "value": 19.42,
            "unit": "ns/op",
            "extra": "63042367 times\n2 procs"
          },
          {
            "name": "BenchmarkRingBufferTracker_ConcurrentObserve - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "63042367 times\n2 procs"
          },
          {
            "name": "BenchmarkRingBufferTracker_ConcurrentObserve - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "63042367 times\n2 procs"
          },
          {
            "name": "BenchmarkTokenBucketBudget_AllowAttempt",
            "value": 66.9,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "17916564 times\n2 procs"
          },
          {
            "name": "BenchmarkTokenBucketBudget_AllowAttempt - ns/op",
            "value": 66.9,
            "unit": "ns/op",
            "extra": "17916564 times\n2 procs"
          },
          {
            "name": "BenchmarkTokenBucketBudget_AllowAttempt - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "17916564 times\n2 procs"
          },
          {
            "name": "BenchmarkTokenBucketBudget_AllowAttempt - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "17916564 times\n2 procs"
          },
          {
            "name": "BenchmarkTokenBucketBudget_Concurrent",
            "value": 80.18,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "14945942 times\n2 procs"
          },
          {
            "name": "BenchmarkTokenBucketBudget_Concurrent - ns/op",
            "value": 80.18,
            "unit": "ns/op",
            "extra": "14945942 times\n2 procs"
          },
          {
            "name": "BenchmarkTokenBucketBudget_Concurrent - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "14945942 times\n2 procs"
          },
          {
            "name": "BenchmarkTokenBucketBudget_Concurrent - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "14945942 times\n2 procs"
          }
        ]
      }
    ]
  }
}