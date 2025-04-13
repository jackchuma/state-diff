LEDGER_ACCOUNT=1

.PHONY: run
run:
	go run . --rpc https://ethereum-full-sepolia-k8s-dev.cbhq.net -o validation.md \
	--ledger --hd-paths "m/44'/60'/$(LEDGER_ACCOUNT)'/0/0" -- ./run.sh
