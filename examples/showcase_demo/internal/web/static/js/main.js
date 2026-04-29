// Mobile menu toggle
document.addEventListener('DOMContentLoaded', function() {
  const menuBtn = document.querySelector('.mobile-menu-btn');
  const navLinks = document.querySelector('.nav-links');
  
  if (menuBtn && navLinks) {
    menuBtn.addEventListener('click', function() {
      navLinks.classList.toggle('mobile-open');
    });
  }
  
  // Smooth scroll for anchor links
  document.querySelectorAll('a[href^="#"]').forEach(anchor => {
    anchor.addEventListener('click', function (e) {
      e.preventDefault();
      const target = document.querySelector(this.getAttribute('href'));
      if (target) {
        target.scrollIntoView({
          behavior: 'smooth',
          block: 'start'
        });
      }
    });
  });
  
  // Add scroll animation for elements
  const observerOptions = {
    root: null,
    rootMargin: '0px',
    threshold: 0.1
  };
  
  const observer = new IntersectionObserver((entries) => {
    entries.forEach(entry => {
      if (entry.isIntersecting) {
        entry.target.classList.add('animate-in');
        observer.unobserve(entry.target);
      }
    });
  }, observerOptions);
  
  // Observe cards and sections
  document.querySelectorAll('.feature-card, .article-card, .blog-card, .team-card, .category-card, .potential-list li, .api-explorer').forEach(el => {
    el.style.opacity = '0';
    el.style.transform = 'translateY(20px)';
    el.style.transition = 'opacity 0.6s cubic-bezier(0.4, 0, 0.2, 1), transform 0.6s cubic-bezier(0.4, 0, 0.2, 1)';
    observer.observe(el);
  });

  // API Explorer Simulation
  const testApiBtn = document.querySelector('.btn-test-api');
  if (testApiBtn) {
    testApiBtn.addEventListener('click', async () => {
      const apiBody = document.querySelector('.api-body code');
      const statusLine = document.querySelector('.status-200');
      
      testApiBtn.disabled = true;
      testApiBtn.textContent = 'Consultando...';
      apiBody.textContent = '// Cargando datos reales de la API...';
      
      try {
        const response = await fetch('/api/stats');
        const data = await response.json();
        
        // Pretty print with delay for effect
        setTimeout(() => {
          apiBody.textContent = JSON.stringify({
            status: "success",
            data: data,
            timestamp: new Date().toISOString()
          }, null, 2);
          statusLine.style.background = '#059669';
          testApiBtn.disabled = false;
          testApiBtn.textContent = '¡API Test Exitoso!';
          setTimeout(() => { testApiBtn.textContent = 'Probar API Real'; }, 3000);
        }, 800);
      } catch (err) {
        apiBody.textContent = '// Error al conectar con la API: ' + err.message;
        testApiBtn.disabled = false;
        testApiBtn.textContent = 'Reintentar';
      }
    });
  }
});

// Add CSS for animation
document.head.insertAdjacentHTML('beforeend', `
  <style>
    .animate-in {
      opacity: 1 !important;
      transform: translateY(0) !important;
    }
    
    @media (max-width: 768px) {
      .nav-links {
        display: none;
        position: absolute;
        top: 64px;
        left: 0;
        right: 0;
        background: white;
        flex-direction: column;
        padding: 1rem;
        border-bottom: 1px solid var(--border);
        box-shadow: var(--shadow);
      }
      
      .nav-links.mobile-open {
        display: flex;
      }
    }
  </style>
`);
