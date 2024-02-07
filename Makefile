build-chatbridge-image: cmd/chatbridge lib/
	docker build --platform linux/amd64,linux/arm64 -f cmd/chatbridge/Containerfile -t trow.k.ichael.dk/chatbridge --push .
