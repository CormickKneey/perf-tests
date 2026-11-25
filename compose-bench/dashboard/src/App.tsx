import Dashboard from "./Dashboard";
import { usePrefs } from "./prefs";
import { BrandMark } from "./ui";

export default function App() {
  const { t } = usePrefs();
  return (
    <>
      <header className="site-header">
        <div className="container bar">
          <a className="brand" href="#/">
            <BrandMark />
            d7y-ci-infra
          </a>
          <nav className="links" aria-label={t.links}>
            <a href="https://d7y.io" target="_blank" rel="noreferrer">
              d7y.io
            </a>
            <a href="https://github.com/dragonflyoss/dragonfly" target="_blank" rel="noreferrer">
              GitHub
            </a>
          </nav>
        </div>
      </header>
      <main id="main">
        <Dashboard />
      </main>
      <footer className="site-footer">
        <div className="container bar">
          <span>d7y-ci-infra</span>
          <span>CormickKneey</span>
        </div>
      </footer>
    </>
  );
}
