import "./globals.css";
import AppShell from "./components/AppShell";

export const metadata = { title: "Nexora — Customer Dashboard" };

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <AppShell>{children}</AppShell>
      </body>
    </html>
  );
}
