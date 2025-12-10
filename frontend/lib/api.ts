/**
 * @description
 * Axios instance configuration for frontend API communication.
 * Handles base URL setup and default headers.
 *
 * @dependencies
 * - axios
 */

import axios from "axios";

const rawApiUrl = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
const API_URL = rawApiUrl.replace(/\/+$/, "");
export const API_BASE_URL = API_URL;

export const api = axios.create({
  baseURL: `${API_URL}/api/v1`,
  headers: {
    "Content-Type": "application/json",
  },
});

export default api;
