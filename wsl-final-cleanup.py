#!/usr/bin/env python3

import os
import re
import subprocess
import sys

def get_all_wsl_violations():
    """Get ALL wsl_v5 violations from both gateway and router packages."""
    try:
        result = subprocess.run([
            'golangci-lint', 'run', './services/gateway/...', './services/router/...', 
            '--config', '.golangci.yml'
        ], capture_output=True, text=True, cwd='/Users/poile/repos/mcp')
        
        violations = []
        for line in result.stdout.split('\n'):
            if 'wsl_v5' in line and ':' in line:
                violations.append(line.strip())
        
        return violations
    except Exception as e:
        print(f"Error getting violations: {e}")
        return []

def fix_trailing_whitespace(file_path, line_number):
    """Fix trailing whitespace violation."""
    try:
        with open(file_path, 'r', encoding='utf-8') as f:
            lines = f.readlines()
        
        if line_number <= len(lines):
            # Remove trailing whitespace from the specific line
            original_line = lines[line_number - 1]
            lines[line_number - 1] = original_line.rstrip() + '\n'
            
            with open(file_path, 'w', encoding='utf-8') as f:
                f.writelines(lines)
            return True
        
    except Exception as e:
        print(f"Error fixing trailing whitespace in {file_path}: {e}")
    
    return False

def fix_all_violations():
    """Fix all remaining wsl_v5 violations comprehensively."""
    violations = get_all_wsl_violations()
    
    if not violations:
        print("No wsl_v5 violations found!")
        return True
        
    print(f"Found {len(violations)} violations to fix")
    
    fixed_count = 0
    
    for violation in violations:
        parts = violation.split(':')
        if len(parts) < 4:
            continue
            
        file_path = parts[0]
        try:
            line_num = int(parts[1])
        except ValueError:
            continue
            
        message = ':'.join(parts[3:]).strip()
        
        print(f"Processing: {file_path}:{line_num} - {message}")
        
        try:
            with open(file_path, 'r', encoding='utf-8') as f:
                lines = f.readlines()
        except:
            print(f"  Cannot read {file_path}")
            continue
            
        # Remove trailing newlines but preserve line structure
        lines = [line.rstrip('\n\r') for line in lines]
        
        modified = False
        
        if 'trailing-whitespace' in message:
            # Handle trailing whitespace
            if line_num - 1 < len(lines):
                original = lines[line_num - 1]
                stripped = original.rstrip()
                if original != stripped:
                    lines[line_num - 1] = stripped
                    modified = True
                    print(f"  Fixed trailing whitespace")
        
        elif 'unnecessary whitespace (leading-whitespace)' in message:
            # Remove unnecessary leading whitespace (empty lines)
            if line_num - 1 < len(lines) and lines[line_num - 1].strip() == '':
                lines.pop(line_num - 1)
                modified = True
                print(f"  Removed unnecessary empty line")
        
        elif 'missing whitespace above this line' in message:
            # Add blank line above
            if line_num - 1 > 0 and line_num - 1 < len(lines):
                if lines[line_num - 2].strip() != '':
                    lines.insert(line_num - 1, '')
                    modified = True
                    print(f"  Added blank line above")
        
        if modified:
            try:
                with open(file_path, 'w', encoding='utf-8') as f:
                    for line in lines:
                        f.write(line + '\n')
                fixed_count += 1
            except Exception as e:
                print(f"  Error writing {file_path}: {e}")
    
    print(f"Fixed violations in {fixed_count} operations")
    
    # Check remaining after fixes
    remaining_violations = get_all_wsl_violations()
    print(f"Remaining violations: {len(remaining_violations)}")
    
    return len(remaining_violations) == 0

def main():
    os.chdir('/Users/poile/repos/mcp')
    
    print("=== FINAL WSL_V5 CLEANUP ===")
    
    # Run multiple iterations until we get them all
    for iteration in range(5):
        print(f"\nIteration {iteration + 1}:")
        
        initial_count = len(get_all_wsl_violations())
        if initial_count == 0:
            print("SUCCESS! All wsl_v5 violations eliminated!")
            return
            
        print(f"Starting with {initial_count} violations")
        
        if fix_all_violations():
            print("SUCCESS! All wsl_v5 violations eliminated!")
            return
        
        # Check if we made progress
        final_count = len(get_all_wsl_violations())
        if final_count >= initial_count:
            print(f"No progress made in iteration {iteration + 1}")
            break
        
        print(f"Progress: {initial_count} -> {final_count}")
    
    # Show remaining violations
    remaining = get_all_wsl_violations()
    if remaining:
        print(f"\nRemaining {len(remaining)} violations:")
        for violation in remaining[:10]:
            print(f"  {violation}")

if __name__ == '__main__':
    main()