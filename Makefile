LEDGER_ACCOUNT=1
RPC=https://eth-mainnet.public.blastapi.io
# RPC=https://ethereum-full-sepolia-k8s-dev.cbhq.net

.PHONY: run
run:
	go run . --rpc $(RPC) -o validation.md \
	--ledger --hd-paths "m/44'/60'/$(LEDGER_ACCOUNT)'/0/0" -- ./run.sh
