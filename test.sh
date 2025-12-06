#!/bin/bash

BIN_DIR=$(dirname "$0")/bin
mkdir -p $BIN_DIR

# Compile generator
go build -o $BIN_DIR/gen gen.go
if [ $? -ne 0 ]; then
    echo "Failed to compile generator"
    exit 1
fi

# Compile main program
go build -o $BIN_DIR/main .
if [ $? -ne 0 ]; then
    echo "Failed to compile main program"
    exit 1
fi

echo "Starting tests..."

for i in {1..100}
do
    # Generate input
    $BIN_DIR/gen > $BIN_DIR/inp.in
    
    # Read N and T from input for info
    read N T < $BIN_DIR/inp.in
    
    # Run main program
    $BIN_DIR/main -silent < $BIN_DIR/inp.in > $BIN_DIR/out.txt 2>&1
    EXIT_CODE=$?
    
    if [ $EXIT_CODE -ne 0 ]; then
        echo "Test $i FAILED: Main program crashed (Exit code $EXIT_CODE)"
        cat $BIN_DIR/inp.in
        cat $BIN_DIR/out.txt
        exit 1
    fi
    
    # Check output for agreement
    # Extract the RESULTS line
    RESULTS=$(grep "RESULTS:" $BIN_DIR/out.txt)
    
    if [ -z "$RESULTS" ]; then
        echo "Test $i FAILED: No RESULTS line found"
        cat $BIN_DIR/inp.in
        cat $BIN_DIR/out.txt
        exit 1
    fi
    
    # Remove "RESULTS: " prefix
    VALS=$(echo "$RESULTS" | sed 's/RESULTS: //')
    
    # Check if all values are the same
    FIRST_VAL=$(echo "$VALS" | awk '{print $1}')
    AGREEMENT=true
    for val in $VALS; do
        if [ "$val" != "$FIRST_VAL" ]; then
            AGREEMENT=false
            break
        fi
    done
    
    if [ "$AGREEMENT" = false ]; then
        echo "Test $i FAILED: No agreement. Output: $VALS"
        cat $BIN_DIR/inp.in
        exit 1
    fi
    
    echo "Test $i PASSED (N=$N, T=$T, Decided=$FIRST_VAL)"
done

echo "All tests passed!"
rm $BIN_DIR/gen $BIN_DIR/main $BIN_DIR/inp.in $BIN_DIR/out.txt