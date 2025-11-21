/**
 * Redirect root "/" to the dashboard view.
 */

import { redirect } from "next/navigation";

export default function HomePage() {
  redirect("/dashboard");
}


