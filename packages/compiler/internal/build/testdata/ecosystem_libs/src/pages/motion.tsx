import { useState } from 'react';
import { motion, AnimatePresence } from 'motion/react';
export default function Page() {
  const [open, setOpen] = useState(true);
  return (
    <main>
      <button id="toggle" onClick={() => setOpen((v) => !v)}>toggle</button>
      <motion.div id="box" initial={{ opacity: 0 }} animate={{ opacity: 1, x: 40 }} transition={{ duration: 0.05 }}>box</motion.div>
      <AnimatePresence>{open ? <motion.p id="pres" key="p" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={{ duration: 0.05 }}>present</motion.p> : null}</AnimatePresence>
    </main>
  );
}
