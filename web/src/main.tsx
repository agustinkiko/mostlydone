import { createRoot } from "react-dom/client";
import { Boot } from "./app/Boot";
import "./styles.css";

const root = document.getElementById("root");
if (root) createRoot(root).render(<Boot />);
