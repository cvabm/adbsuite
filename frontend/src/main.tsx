import React from "react";
import { createRoot } from "react-dom/client";
import "./style.css";
import App from "./App";

const container = document.getElementById("root");
const root = createRoot(container!);
// StrictMode 会双挂载，导致启动时重复拉设备/闪烁，生产式桌面工具不用
root.render(<App />);
