#!/bin/sh
# Clone the benchmark corpus at the exact commits used for the published results.
# Usage: bench/corpus.sh [dir]   (default: $AGENTBRIEF_CORPUS or ~/agentbrief-corpus)
set -eu
dir=${1:-${AGENTBRIEF_CORPUS:-$HOME/agentbrief-corpus}}
mkdir -p "$dir"
clone() { # name url sha
	[ -d "$dir/$1" ] && return
	git init -q "$dir/$1" && git -C "$dir/$1" fetch -q --depth 1 "$2" "$3" && git -C "$dir/$1" checkout -q FETCH_HEAD
}
clone chi https://github.com/go-chi/chi.git 167e1e3bd039d060696b99c8da4e876ae04f42c1
clone cobra https://github.com/spf13/cobra.git adbc8813901bba65827259daa8e22ff94ec1f30e
clone codex https://github.com/openai/codex.git 72f999edc7848961b99c4b98bf1e62d12fa35798
clone excalidraw https://github.com/excalidraw/excalidraw.git 2df993fb397dd6d64625908c9c10337727617f6f
clone fastapi https://github.com/fastapi/fastapi.git f5c6e9b4f9cadc1bf0f9a1fd672e621206b63007
clone flask https://github.com/pallets/flask.git d086db856be187255b8ec61ef409357393020f32
clone gson https://github.com/google/gson.git 67427c40f7306276e63e09c8bb66fefb90429d86
clone guzzle https://github.com/guzzle/guzzle 93939470950a9b11e2e84204166ef5e048c55fe4
clone hono https://github.com/honojs/hono.git 552ea024250093a48071b3ded9710de4c6e05f14
clone httpx https://github.com/encode/httpx.git b5addb64f0161ff6bfe94c124ef76f6a1fba5254
clone Newtonsoft.Json https://github.com/JamesNK/Newtonsoft.Json 52fa3aef1f2cadcd3a3f874251eddc98d3efbbaa
clone plug https://github.com/elixir-plug/plug 73404f851852a00ffb2014be95d4598900fa77b8
clone ripgrep https://github.com/BurntSushi/ripgrep.git 3fce3b5bb0236da2df6d99672afb8a719642eca7
clone ruff https://github.com/astral-sh/ruff.git eb625e43e489bc090aae9c4aa4a407cf389f65d0
clone sinatra https://github.com/sinatra/sinatra.git cb22afd7902b566b6eaba6c4ea89739494a65d12
clone spring-petclinic https://github.com/spring-projects/spring-petclinic.git 500158f732419217507c7656904b8e6aa1bcc0d6
clone vite https://github.com/vitejs/vite.git 3b72997d42e7dd2b31898d0e68461e3025e836b6
