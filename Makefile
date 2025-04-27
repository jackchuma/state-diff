LEDGER_ACCOUNT=1
RPC=https://eth-mainnet.public.blastapi.io
# RPC=https://ethereum-full-sepolia-k8s-dev.cbhq.net

.PHONY: run
run:
	go run . --rpc $(RPC) -o validation.md \
	-- ./run.sh --sender 0x9986ccaf9e3de0ffef82a0f7fa3a06d5afe07252
