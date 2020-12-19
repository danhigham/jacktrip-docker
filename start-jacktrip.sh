#!/bin/bash
jackd -ddummy -r48000 -p512 &
sleep 5 &&
jacktrip -S -p$HUB_PATCH
