import { Route, Routes, useParams } from "react-router-dom";
import { Board } from "./pages/Board";
import { Live } from "./pages/Live";
import { Login } from "./pages/Login";
import { Me } from "./pages/Me";
import { Repos } from "./pages/Repos";
import { Review } from "./pages/Review";
import { RiskPolicy } from "./pages/RiskPolicy";
import { parseFeaturePath, parseRiskPath } from "./routes";

/** Trang tính năng: /f/<repo>/<key> là review, thêm /live là theo dõi agent. */
function FeaturePage() {
  const { repoId, key, live } = parseFeaturePath(useParams()["*"] ?? "");
  if (live) return <Live repoId={repoId} featureKey={key} />;
  return <Review repoId={repoId} featureKey={key} />;
}

/** Trang chính sách rủi ro: /risk/<repo nhiều đoạn>, tab theo hash. */
function RiskPage() {
  return <RiskPolicy repoId={parseRiskPath(useParams()["*"] ?? "")} />;
}

export function App() {
  return (
    <Routes>
      <Route path="/" element={<Board />} />
      <Route path="/login" element={<Login />} />
      <Route path="/me" element={<Me />} />
      <Route path="/repos" element={<Repos />} />
      <Route path="/f/*" element={<FeaturePage />} />
      <Route path="/risk/*" element={<RiskPage />} />
      <Route path="*" element={<Board />} />
    </Routes>
  );
}
