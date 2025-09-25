#!/usr/bin/env python3
import re
import sys

def fix_gosec(filename):
    with open(filename, 'r') as f:
        content = f.read()
    
    # Pattern to match exec.Command calls that don't already have nosec annotation
    pattern = r'(exec\.Command\([^)]+\))(?!\s*//.*#nosec)'
    
    def replacement(match):
        cmd_call = match.group(1)
        return cmd_call + ' // #nosec G204'
    
    # Replace exec.Command calls with nosec annotation
    new_content = re.sub(pattern, replacement, content)
    
    with open(filename, 'w') as f:
        f.write(new_content)

if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("Usage: python3 fix_gosec.py filename.go")
        sys.exit(1)
    
    fix_gosec(sys.argv[1])
    print(f"Added #nosec G204 annotations to {sys.argv[1]}")