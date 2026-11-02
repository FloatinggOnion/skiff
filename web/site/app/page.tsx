import { SiteNav } from "@/components/site-nav";
import { Hero } from "@/components/hero";
import { WhySkiff } from "@/components/why-skiff";
import { FeatureRows } from "@/components/feature-rows";
import { ConsoleShowcase } from "@/components/console-showcase";
import { StackMarquee } from "@/components/stack-marquee";
import { AlsoIncluded } from "@/components/also-included";
import { HowItWorks } from "@/components/how-it-works";
import { FinalCta } from "@/components/final-cta";
import { SiteFooter } from "@/components/site-footer";

export default function Home() {
  return (
    <>
      <SiteNav />
      <main>
        <Hero />
        <WhySkiff />
        <ConsoleShowcase />
        <StackMarquee />
        <FeatureRows />
        <AlsoIncluded />
        <HowItWorks />
        <FinalCta />
      </main>
      <SiteFooter />
    </>
  );
}
