module sourcecraft-wrapper-service

go 1.26.4

require (
	github.com/joho/godotenv v1.5.1
	google.golang.org/grpc v1.84.0
	google.golang.org/protobuf v1.36.12
)

require (
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
)

require repo-therapist/runtimeconfig v0.0.0

replace repo-therapist/runtimeconfig => ../runtimeconfig
