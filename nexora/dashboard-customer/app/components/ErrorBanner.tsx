export default function ErrorBanner({ message }: { message: string | null }) {
  if (!message) return null;
  return <div className="error-box">{message}</div>;
}
