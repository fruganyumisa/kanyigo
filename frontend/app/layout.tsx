import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Mail Log Dashboard",
  description: "Operational dashboard for Postfix and Amavis mail logs"
};

export default function RootLayout({
  children
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
