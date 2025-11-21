/**
 * @description
 * Utility functions for the frontend application.
 * Primarily used for class name merging in Shadcn UI components.
 *
 * @dependencies
 * - clsx: For constructing className strings conditionally.
 * - tailwind-merge: For merging Tailwind CSS classes without conflicts.
 */

import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

/**
 * cn combines class names using clsx and then merges them using tailwind-merge
 * to resolve conflicting Tailwind classes (e.g., 'px-2' and 'px-4').
 * 
 * @param inputs - Variable number of class values (strings, objects, arrays, etc.)
 * @returns A single merged class string.
 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

