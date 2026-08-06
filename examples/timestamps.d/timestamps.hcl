job "echoloop_test" {
  command = "/bin/bash"
  args = [
    "-c",
    "while true ; do echo 'test'; sleep 10; done"
  ]

  stdout = "test.log"
  stderr = "test_error.log"
  enableTimestamps = true
  timestampFormat = "RFC1123"
}

job "echoloop_custom" {
  command = "/bin/bash"
  args = [
    "-c",
    "while true ; do echo 'test'; sleep 10; done"
  ]

  stdout = "test_custom.log"
  stderr = "test_custom_error.log"
  enableTimestamps = true
  customTimestampFormat = "2006-01-02 15:04:05"
}

job "echoloop_kitchentime" {
  command = "/bin/bash"
  args = [
    "-c",
    "while true ; do echo 'test'; sleep 10; done"
  ]

  stdout = "test_kitchentime.log"
  stderr = "test_kitchentime_error.log"
  enableTimestamps = true
  timestampFormat = "Kitchen"
}

job "echoloop_notime" {
  command = "/bin/bash"
  args = [
    "-c",
    "while true ; do echo 'test'; sleep 10; done"
  ]

  stdout = "test_notime.log"
  stderr = "test_notime_error.log"
}

# opts out of the default decoration entirely: output is written to the
# targets directly, byte-identical
job "echoloop_raw" {
  command = "/bin/bash"
  args = [
    "-c",
    "while true ; do echo 'test'; sleep 10; done"
  ]

  stdout = "test_raw.log"
  stderr = "test_raw_error.log"
  enableTimestamps = false
  enableNamePrefix = false
}

job "echoloop_nameprefix" {
  command = "/bin/bash"
  args = [
    "-c",
    "while true ; do echo 'test'; sleep 10; done"
  ]

  stdout = "test_nameprefix.log"
  stderr = "test_nameprefix_error.log"
  enableTimestamps = true
  enableNamePrefix = true
}
