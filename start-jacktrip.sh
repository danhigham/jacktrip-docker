#!/bin/bash
jackd -d dummy &
jacktrip -s --localaddress 0.0.0.0
