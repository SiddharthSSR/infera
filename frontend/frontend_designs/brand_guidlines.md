The code below contains a design. This design should be used to create a new app or be added to an existing one.

Look at the current open project to determine if a project exists. If no project is open, create a new Vite project then create this view in React after componentizing it.

If a project does exist, determine the framework being used and implement the design within that framework. Identify whether reusable components already exist that can be used to implement the design faithfully and if so use them, otherwise create new components. If other views already exist in the project, make sure to place the view in a sensible route and connect it to the other views.

Ensure the visual characteristics, layout, and interactions in the design are preserved with perfect fidelity.

Run the dev command so the user can see the app once finished.

```
<html lang="en" vid="0"><head vid="1">
    <meta charset="UTF-8" vid="2">
    <meta name="viewport" content="width=device-width, initial-scale=1.0" vid="3">
    <title vid="4">Brand Guidelines | INFERA.AI</title>
    <style vid="5">
        :root {
            --bg-paper: #FDFBF8;
            --bg-accent: #F4F2EE;
            --text-primary: #050505;
            --text-secondary: #555555;
            --border-color: #D8D6D4;
            --grid-line: 1px solid var(--border-color);
            --font-main: "DM Sans", "Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
            --font-mono: "Space Mono", monospace;
            
            --color-success: #2E7D32;
            --color-warning: #F9A825;
            --color-error: #C62828;
        }

        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
            -webkit-font-smoothing: antialiased;
        }

        body {
            background-color: var(--bg-paper);
            color: var(--text-primary);
            font-family: var(--font-main);
            display: flex;
            flex-direction: column;
            min-height: 100vh;
            border-left: var(--grid-line);
            border-right: var(--grid-line);
            max-width: 1280px;
            margin: 0 auto;
            line-height: 1.4;
            border-radius: 20px;
            overflow-x: hidden;
        }

        
        h1, h2, h3, h4 {
            font-weight: 500;
            color: var(--text-primary);
            letter-spacing: -0.02em;
        }

        .display-text {
            font-size: 5rem;
            line-height: 0.9;
            text-transform: uppercase;
            font-weight: 600;
            letter-spacing: -0.04em;
            padding: 3rem 0;
            text-align: center;
            border-bottom: var(--grid-line);
        }

        .label-text {
            font-size: 0.65rem;
            text-transform: uppercase;
            letter-spacing: 0.1em;
            color: var(--text-secondary);
            font-weight: 600;
            display: flex;
            align-items: center;
            gap: 6px;
            margin-bottom: 0.75rem;
        }

        .mono {
            font-family: var(--font-mono);
        }

        
        a {
            color: inherit;
            text-decoration: none;
            transition: opacity 0.2s;
        }
        
        a:hover { opacity: 0.6; }

        .top-nav {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 1.25rem 2rem;
            border-bottom: var(--grid-line);
        }

        .nav-group {
            display: flex;
            gap: 2rem;
            align-items: center;
        }

        .nav-link {
            font-size: 0.7rem;
            text-transform: uppercase;
            letter-spacing: 0.15em;
            font-weight: 600;
        }

        .nav-diamond {
            font-size: 0.6rem;
            color: var(--text-secondary);
        }

        
        .grid-row {
            display: grid;
            grid-template-columns: repeat(4, 1fr);
            border-bottom: var(--grid-line);
        }

        .cell {
            padding: 2rem;
            border-right: var(--grid-line);
            position: relative;
            display: flex;
            flex-direction: column;
        }

        .cell:last-child { border-right: none; }
        .grid-col-2 { grid-column: span 2; }
        .grid-col-3 { grid-column: span 3; }
        .grid-col-4 { grid-column: span 4; }

        
        .color-swatch-container {
            display: flex;
            flex-direction: column;
            gap: 1rem;
            height: 100%;
        }

        .swatch {
            height: 80px;
            width: 100%;
            border: 1px solid var(--border-color);
            position: relative;
            padding: 1rem;
            display: flex;
            flex-direction: column;
            justify-content: flex-end;
        }

        .swatch-label {
            font-family: var(--font-mono);
            font-size: 0.75rem;
            display: flex;
            justify-content: space-between;
        }

        
        .type-sample {
            margin-bottom: 2rem;
        }
        .type-sample:last-child { margin-bottom: 0; }
        
        .type-meta {
            font-family: var(--font-mono);
            font-size: 0.7rem;
            color: var(--text-secondary);
            margin-bottom: 0.5rem;
            display: block;
        }

        
        .action-btn {
            font-size: 0.7rem;
            text-transform: uppercase;
            letter-spacing: 0.15em;
            background: none;
            border: none;
            padding: 0;
            cursor: pointer;
            border-bottom: 1px solid var(--text-primary);
            padding-bottom: 2px;
            font-weight: 600;
            display: inline-block;
        }

        .control-input {
            width: 100%;
            border: none;
            border-bottom: 1px solid var(--text-primary);
            background: transparent;
            padding: 0.5rem 0;
            font-family: var(--font-main);
            font-size: 1rem;
            outline: none;
        }

        .status-dot {
            width: 8px;
            height: 8px;
            border-radius: 50%;
            display: inline-block;
        }

        .badge {
            font-size: 0.65rem;
            padding: 2px 6px;
            border: 1px solid var(--border-color);
            text-transform: uppercase;
            border-radius: 2px;
            font-weight: 600;
            display: inline-block;
        }

        .icon {
            width: 20px;
            height: 20px;
            stroke: currentColor;
            stroke-width: 1.5;
            fill: none;
        }

        
        .spec-list {
            list-style: none;
            font-family: var(--font-mono);
            font-size: 0.8rem;
            color: var(--text-secondary);
        }
        .spec-list li {
            padding: 0.5rem 0;
            border-bottom: 1px solid #EEEEEC;
            display: flex;
            justify-content: space-between;
        }
        .spec-list li:last-child { border-bottom: none; }
        .spec-val { color: var(--text-primary); }

        @media (max-width: 1024px) {
            .grid-row { grid-template-columns: repeat(2, 1fr); }
            .grid-col-1, .grid-col-2, .grid-col-3 { grid-column: span 2; }
            .display-text { font-size: 3rem; }
        }
    </style>
    <link href="https://fonts.googleapis.com/css2?family=DM+Sans:wght@400;500;600;700&amp;family=Space+Mono&amp;display=swap" rel="stylesheet" vid="6">
</head>
<body vid="7">

    
    <nav class="top-nav" vid="8">
        <div style="font-weight: 700; letter-spacing: -0.02em;" vid="9">INFERA.AI</div>
        <div class="nav-group" vid="10">
            <a href="#" class="nav-link" vid="11">Design System</a>
            <span class="nav-diamond" vid="12">◇</span>
            <a href="#" class="nav-link" vid="13">Tokens</a>
            <span class="nav-diamond" vid="14">◇</span>
            <a href="#" class="nav-link" vid="15">Components</a>
            <span class="nav-diamond" vid="16">◇</span>
            <a href="#" class="nav-link" vid="17">Patterns</a>
        </div>
        <div class="nav-group" style="gap: 1rem;" vid="18">
            <a href="#" class="nav-link" vid="19">v2.4</a>
            <a href="#" class="nav-link" vid="20">DOWNLOAD ASSETS</a>
        </div>
    </nav>

    
    <header class="display-text" vid="21">
        Brand Guidelines
    </header>

    
    <div class="grid-row" vid="22">
        <div class="cell grid-col-2" vid="23">
            <div class="label-text" vid="24">Brand Philosophy</div>
            <h2 style="font-size: 1.75rem; margin-top: 1rem; line-height: 1.2;" vid="25">
                Technical. Minimal. Precise.
            </h2>
            <p style="margin-top: 1.5rem; color: var(--text-secondary); max-width: 90%;" vid="26">
                The INFERA.AI aesthetic is rooted in the precision of engineering schematics and the warmth of architectural paper. We prioritize clarity of data, rigid structural grids, and high-contrast typography to convey reliability in AI infrastructure.
            </p>
        </div>
        <div class="cell" style="background-color: var(--bg-accent);" vid="27">
            <div class="label-text" vid="28">Voice &amp; Tone</div>
            <ul class="spec-list" style="margin-top: 1rem;" vid="29">
                <li vid="30"><span vid="31">Confident</span> <span class="spec-val" vid="32">Not arrogant</span></li>
                <li vid="33"><span vid="34">Concise</span> <span class="spec-val" vid="35">No fluff</span></li>
                <li vid="36"><span vid="37">Technical</span> <span class="spec-val" vid="38">High precision</span></li>
                <li vid="39"><span vid="40">Human</span> <span class="spec-val" vid="41">Warm foundation</span></li>
            </ul>
        </div>
        <div class="cell" vid="42">
            <div class="label-text" vid="43">Grid System</div>
            <div style="margin-top: auto;" vid="44">
                <div style="display: grid; grid-template-columns: repeat(4, 1fr); gap: 4px; height: 60px; opacity: 0.3;" vid="45">
                    <div style="background: var(--text-primary);" vid="46"></div>
                    <div style="background: var(--text-primary);" vid="47"></div>
                    <div style="background: var(--text-primary);" vid="48"></div>
                    <div style="background: var(--text-primary);" vid="49"></div>
                </div>
                <div class="mono" style="font-size: 0.75rem; margin-top: 0.5rem; color: var(--text-secondary);" vid="50">4-Column Flexible Grid</div>
            </div>
        </div>
    </div>

    
    <div class="grid-row" vid="51">
        <div class="cell" vid="52">
            <div class="label-text" vid="53">Primary Backgrounds</div>
            <div class="color-swatch-container" style="margin-top: 1rem;" vid="54">
                <div class="swatch" style="background-color: var(--bg-paper);" vid="55">
                    <div class="swatch-label" vid="56"><span vid="57">Paper</span> <span vid="58">#FDFBF8</span></div>
                </div>
                <div class="swatch" style="background-color: var(--bg-accent);" vid="59">
                    <div class="swatch-label" vid="60"><span vid="61">Accent</span> <span vid="62">#F4F2EE</span></div>
                </div>
            </div>
        </div>
        <div class="cell" vid="63">
            <div class="label-text" vid="64">Primary Text &amp; UI</div>
            <div class="color-swatch-container" style="margin-top: 1rem;" vid="65">
                <div class="swatch" style="background-color: var(--text-primary); color: white;" vid="66">
                    <div class="swatch-label" vid="67"><span vid="68">Ink Black</span> <span vid="69">#050505</span></div>
                </div>
                <div class="swatch" style="background-color: var(--text-secondary); color: white;" vid="70">
                    <div class="swatch-label" vid="71"><span vid="72">Muted</span> <span vid="73">#555555</span></div>
                </div>
                <div class="swatch" style="background-color: var(--border-color);" vid="74">
                    <div class="swatch-label" vid="75"><span vid="76">Border</span> <span vid="77">#D8D6D4</span></div>
                </div>
            </div>
        </div>
        <div class="cell grid-col-2" vid="78">
            <div class="label-text" vid="79">Semantic Colors</div>
            <div style="display: grid; grid-template-columns: repeat(3, 1fr); gap: 1rem; margin-top: 1rem; height: 100%;" vid="80">
                <div class="swatch" style="background-color: var(--color-success); color: white; border: none;" vid="81">
                    <div class="swatch-label" vid="82"><span vid="83">Operational</span> <span vid="84">#2E7D32</span></div>
                </div>
                <div class="swatch" style="background-color: var(--color-warning); color: black; border: none;" vid="85">
                    <div class="swatch-label" vid="86"><span vid="87">Warning</span> <span vid="88">#F9A825</span></div>
                </div>
                <div class="swatch" style="background-color: var(--color-error); color: white; border: none;" vid="89">
                    <div class="swatch-label" vid="90"><span vid="91">Critical</span> <span vid="92">#C62828</span></div>
                </div>
            </div>
        </div>
    </div>

    
    <div class="grid-row" vid="93">
        <div class="cell grid-col-2" vid="94">
            <div class="label-text" vid="95">Typography Hierarchy</div>
            <div style="display: flex; flex-direction: column; gap: 2rem; margin-top: 1rem;" vid="96">
                <div class="type-sample" vid="97">
                    <span class="type-meta" vid="98">Display Header / DM Sans / 6rem / Uppercase / -0.04em</span>
                    <div style="font-size: 3.5rem; line-height: 0.9; font-weight: 600; letter-spacing: -0.04em; text-transform: uppercase;" vid="99">
                        Inference Engine
                    </div>
                </div>
                <div class="type-sample" vid="100">
                    <span class="type-meta" vid="101">Section Title / DM Sans / 1.75rem / Regular / -0.02em</span>
                    <div style="font-size: 1.75rem; letter-spacing: -0.02em;" vid="102">
                        Deploy scalable models with zero latency.
                    </div>
                </div>
                <div class="type-sample" vid="103">
                    <span class="type-meta" vid="104">Body Copy / DM Sans / 1rem / Regular</span>
                    <div style="font-size: 1rem; color: var(--text-secondary); max-width: 480px;" vid="105">
                        Our infrastructure allows for dynamic scaling of H100 GPU nodes across multiple regions. The system automatically balances load based on predictive traffic patterns.
                    </div>
                </div>
            </div>
        </div>
        <div class="cell grid-col-2" style="background-color: var(--bg-accent);" vid="106">
            <div class="label-text" vid="107">Utility &amp; Data Typography</div>
            <div style="display: flex; flex-direction: column; gap: 2rem; margin-top: 1rem;" vid="108">
                <div class="type-sample" vid="109">
                    <span class="type-meta" vid="110">Label Text / DM Sans / 0.65rem / Uppercase / +0.1em / Bold</span>
                    <div class="label-text" style="margin-bottom: 0;" vid="111">ACTIVE NODES</div>
                </div>
                <div class="type-sample" vid="112">
                    <span class="type-meta" vid="113">Nav Link / DM Sans / 0.7rem / Uppercase / +0.15em / SemiBold</span>
                    <span class="nav-link" style="border-bottom: 1px solid currentColor;" vid="114">DASHBOARD</span>
                </div>
                <div class="type-sample" vid="115">
                    <span class="type-meta" vid="116">Mono Data / Space Mono / 0.85rem</span>
                    <div class="mono" style="padding: 1rem; background: rgba(0,0,0,0.03); border: 1px solid var(--border-color);" vid="117">
                        id: "node-us-east-01"<br vid="118">
                        status: "operational"<br vid="119">
                        uptime: 99.98%
                    </div>
                </div>
            </div>
        </div>
    </div>

    
    <div class="grid-row" style="border-bottom: none;" vid="120">
        <div class="cell" vid="121">
            <div class="label-text" vid="122">Buttons &amp; Links</div>
            <div style="display: flex; flex-direction: column; gap: 1.5rem; margin-top: 1rem;" vid="123">
                <div vid="124">
                    <button class="action-btn" vid="125">Primary Action</button>
                    <div class="mono" style="font-size: 0.7rem; color: var(--text-secondary); margin-top: 0.5rem;" vid="126">.action-btn</div>
                </div>
                <div vid="127">
                    <button class="action-btn" style="color: var(--color-error); border-bottom-color: var(--color-error);" vid="128">Destructive</button>
                    <div class="mono" style="font-size: 0.7rem; color: var(--text-secondary); margin-top: 0.5rem;" vid="129">.btn-revoke</div>
                </div>
                <div vid="130">
                    <div style="font-size: 0.7rem; text-decoration: underline; text-underline-offset: 3px; cursor: pointer;" vid="131">Inline Link</div>
                </div>
            </div>
        </div>
        <div class="cell" vid="132">
            <div class="label-text" vid="133">Form Elements</div>
            <div style="margin-top: 1rem;" vid="134">
                <input type="text" class="control-input" placeholder="Input field placeholder" vid="135">
                <div class="mono" style="font-size: 0.7rem; color: var(--text-secondary); margin-top: 0.5rem;" vid="136">Standard Input (Underline only)</div>
                
                <div style="margin-top: 2rem; display: flex; align-items: center; gap: 0.5rem;" vid="137">
                    <input type="checkbox" checked="" vid="138">
                    <span style="font-size: 0.9rem;" vid="139">Checkbox Option</span>
                </div>
            </div>
        </div>
        <div class="cell" vid="140">
            <div class="label-text" vid="141">Status &amp; Badges</div>
            <div style="margin-top: 1rem; display: flex; flex-direction: column; gap: 1rem;" vid="142">
                <div style="display: flex; align-items: center; gap: 0.5rem; font-size: 0.9rem;" vid="143">
                    <span class="status-dot" style="background: var(--color-success);" vid="144"></span> Active
                </div>
                <div style="display: flex; align-items: center; gap: 0.5rem; font-size: 0.9rem;" vid="145">
                    <span class="status-dot" style="background: var(--color-warning);" vid="146"></span> Warning
                </div>
                <div style="margin-top: 0.5rem;" vid="147">
                    <span class="badge" vid="148">4-BIT (AWQ)</span>
                    <span class="badge" style="margin-left: 0.5rem;" vid="149">PRODUCTION</span>
                </div>
            </div>
        </div>
        <div class="cell" vid="150">
            <div class="label-text" vid="151">Iconography</div>
            <div style="margin-top: 1rem; display: grid; grid-template-columns: repeat(4, 1fr); gap: 1rem;" vid="152">
                <svg class="icon" viewBox="0 0 24 24" vid="153"><path d="M12 2v20M2 12h20" vid="154"></path></svg>
                <svg class="icon" viewBox="0 0 24 24" vid="155"><circle cx="12" cy="12" r="10" vid="156"></circle><polyline points="12 6 12 12 16 14" vid="157"></polyline></svg>
                <svg class="icon" viewBox="0 0 24 24" vid="158"><rect x="2" y="3" width="20" height="14" rx="2" ry="2" vid="159"></rect><line x1="8" y1="21" x2="16" y2="21" vid="160"></line><line x1="12" y1="17" x2="12" y2="21" vid="161"></line></svg>
                <svg class="icon" viewBox="0 0 24 24" vid="162"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" vid="163"></path></svg>
            </div>
            <p style="margin-top: 1.5rem; font-size: 0.8rem; color: var(--text-secondary);" vid="164">
                Icons are strictly 1.5px stroke, no fill. Sharp or minimal rounded corners. Used sparingly to denote data categories.
            </p>
        </div>
    </div>


</body></html>
```
