#!/bin/bash

# SlimServe Benchmark Runner
# Comprehensive performance testing script for critical code paths

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
BENCHMARK_TIME=${BENCHMARK_TIME:-"5s"}
OUTPUT_DIR=${OUTPUT_DIR:-"benchmark_results"}
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")

# Create output directory
mkdir -p "$OUTPUT_DIR"

echo -e "${BLUE}SlimServe Performance Benchmark Suite${NC}"
echo -e "${BLUE}=====================================${NC}"
echo "Timestamp: $(date)"
echo "Benchmark time: $BENCHMARK_TIME"
echo "Output directory: $OUTPUT_DIR"
echo ""

# Function to run benchmark and save results
run_benchmark() {
    local name=$1
    local package=$2
    local pattern=$3
    local output_file="$OUTPUT_DIR/${name}_${TIMESTAMP}.txt"
    
    echo -e "${YELLOW}Running $name benchmarks...${NC}"
    echo "Package: $package"
    echo "Pattern: $pattern"
    echo "Output: $output_file"
    echo ""
    
    # Run benchmark and save to file
    go test -bench="$pattern" -benchmem -benchtime="$BENCHMARK_TIME" "$package" | tee "$output_file"
    
    # Check if benchmark completed successfully
    if [ ${PIPESTATUS[0]} -eq 0 ]; then
        echo -e "${GREEN}✓ $name benchmarks completed successfully${NC}"
    else
        echo -e "${RED}✗ $name benchmarks failed${NC}"
        return 1
    fi
    echo ""
}

# Function to extract key metrics
extract_metrics() {
    local file=$1
    local name=$2
    
    echo -e "${BLUE}Key Metrics for $name:${NC}"
    
    # Extract benchmark results (lines containing "Benchmark")
    grep "^Benchmark" "$file" | while read -r line; do
        # Parse benchmark line: BenchmarkName-cores iterations ns/op memory allocs/op
        if [[ $line =~ ^(Benchmark[^-]+).*[[:space:]]([0-9]+)[[:space:]]+ns/op[[:space:]]+([0-9]+)[[:space:]]+B/op[[:space:]]+([0-9]+)[[:space:]]+allocs/op ]]; then
            local bench_name="${BASH_REMATCH[1]}"
            local ns_per_op="${BASH_REMATCH[2]}"
            local bytes_per_op="${BASH_REMATCH[3]}"
            local allocs_per_op="${BASH_REMATCH[4]}"
            
            # Convert nanoseconds to more readable units
            if [ "$ns_per_op" -gt 1000000 ]; then
                local time_unit="ms"
                local time_value=$(echo "scale=2; $ns_per_op / 1000000" | bc -l)
            elif [ "$ns_per_op" -gt 1000 ]; then
                local time_unit="μs"
                local time_value=$(echo "scale=2; $ns_per_op / 1000" | bc -l)
            else
                local time_unit="ns"
                local time_value="$ns_per_op"
            fi
            
            # Convert bytes to more readable units
            if [ "$bytes_per_op" -gt 1048576 ]; then
                local mem_unit="MB"
                local mem_value=$(echo "scale=2; $bytes_per_op / 1048576" | bc -l)
            elif [ "$bytes_per_op" -gt 1024 ]; then
                local mem_unit="KB"
                local mem_value=$(echo "scale=2; $bytes_per_op / 1024" | bc -l)
            else
                local mem_unit="B"
                local mem_value="$bytes_per_op"
            fi
            
            printf "  %-40s %8s %-2s %8s %-2s %6s allocs\n" \
                "$bench_name" "$time_value" "$time_unit" "$mem_value" "$mem_unit" "$allocs_per_op"
        fi
    done
    echo ""
}

# Function to generate summary report
generate_summary() {
    local summary_file="$OUTPUT_DIR/summary_${TIMESTAMP}.md"
    
    echo -e "${BLUE}Generating summary report...${NC}"
    
    cat > "$summary_file" << EOF
# SlimServe Benchmark Summary

**Date:** $(date)  
**Benchmark Duration:** $BENCHMARK_TIME  
**System:** $(uname -a)  
**Go Version:** $(go version)

## Results Overview

EOF

    # Add results for each benchmark category
    for result_file in "$OUTPUT_DIR"/*_"$TIMESTAMP".txt; do
        if [ -f "$result_file" ]; then
            local category=$(basename "$result_file" "_${TIMESTAMP}.txt")
            echo "### $category" >> "$summary_file"
            echo "" >> "$summary_file"
            echo '```' >> "$summary_file"
            grep "^Benchmark" "$result_file" >> "$summary_file"
            echo '```' >> "$summary_file"
            echo "" >> "$summary_file"
        fi
    done
    
    echo "Summary report saved to: $summary_file"
}

# Main execution
main() {
    echo -e "${YELLOW}Starting benchmark suite...${NC}"
    echo ""
    
    # Check if bc is available for calculations
    if ! command -v bc &> /dev/null; then
        echo -e "${YELLOW}Warning: 'bc' not found. Metric calculations will be skipped.${NC}"
        echo ""
    fi
    
    # Run cache benchmarks
    run_benchmark "cache" "./internal/files" "BenchmarkCache"
    
    # Run thumbnail benchmarks  
    run_benchmark "thumbnail" "./internal/files" "BenchmarkGenerate|BenchmarkThumbnail"
    
    # Run server benchmarks
    run_benchmark "server" "./internal/server" "BenchmarkServe|BenchmarkContains|BenchmarkTryServe"
    
    # Run middleware benchmarks
    run_benchmark "middleware" "./internal/server" "BenchmarkAccess|BenchmarkSession|BenchmarkCreate|BenchmarkPath|BenchmarkRoute|BenchmarkConcurrent|BenchmarkMiddleware"
    
    # Extract and display key metrics
    if command -v bc &> /dev/null; then
        echo -e "${BLUE}Performance Summary${NC}"
        echo -e "${BLUE}==================${NC}"
        
        for result_file in "$OUTPUT_DIR"/*_"$TIMESTAMP".txt; do
            if [ -f "$result_file" ]; then
                local category=$(basename "$result_file" "_${TIMESTAMP}.txt")
                extract_metrics "$result_file" "$category"
            fi
        done
    fi
    
    # Generate summary report
    generate_summary
    
    echo -e "${GREEN}✓ All benchmarks completed successfully!${NC}"
    echo -e "${BLUE}Results saved in: $OUTPUT_DIR${NC}"
}

# Help function
show_help() {
    cat << EOF
SlimServe Benchmark Runner

Usage: $0 [OPTIONS]

Options:
    -h, --help          Show this help message
    -t, --time TIME     Set benchmark duration (default: 5s)
    -o, --output DIR    Set output directory (default: benchmark_results)

Environment Variables:
    BENCHMARK_TIME      Benchmark duration (e.g., "10s", "1m")
    OUTPUT_DIR          Output directory for results

Examples:
    $0                          # Run with defaults
    $0 -t 10s                   # Run for 10 seconds each
    $0 -o results               # Save to 'results' directory
    BENCHMARK_TIME=1m $0        # Run for 1 minute each

EOF
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -t|--time)
            BENCHMARK_TIME="$2"
            shift 2
            ;;
        -o|--output)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            show_help
            exit 1
            ;;
    esac
done

# Run main function
main
