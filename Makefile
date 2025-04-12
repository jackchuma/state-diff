.PHONY: run
run:
	go run . -rpc https://ethereum-full-sepolia-k8s-dev.cbhq.net -o validation.md
