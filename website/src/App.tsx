import { Download } from "./components/Download";
import { Features } from "./components/Features";
import { Footer } from "./components/Footer";
import { Hero } from "./components/Hero";
import { Nav } from "./components/Nav";

function App() {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <Nav />
      <main>
        <Hero />
        <Features />
        <Download />
      </main>
      <Footer />
    </div>
  );
}

export default App;
